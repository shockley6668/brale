"""
BraleSharedStrategy - 示例策略
--------------------------------
从 Brale 写入的 SQLite (trade_risk 表) 中读取止盈/止损，结合
freqtrade 的 custom_stoploss/custom_exit 钩子，实现动态调价。

配置：
  export BRALE_RISK_DB=/freqtrade/shared/trade_risk.db
  export BRALE_RISK_REFRESH=2   # polling 间隔秒
"""

import logging
import os
import sqlite3
import threading
import time
from typing import Dict, Optional

from freqtrade.strategy import IStrategy

logger = logging.getLogger(__name__)


class BraleSharedStrategy(IStrategy):
    """
    仅作为示例，不包含实际入场逻辑。主要展示如何同步 Brale 的
    止盈/止损信息（trade_risk 表），并在 custom_stoploss/custom_exit
    中应用。
    """

    minimal_roi = {"0": 10}
    stoploss = -0.99
    timeframe = "5m"

    startup_candle_count = 50

    plot_config = {}
    use_custom_stoploss = True
    use_custom_exit = True

    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self._risk_db_path = os.getenv("BRALE_RISK_DB", "/freqtrade/shared/trade_risk.db")
        self._refresh_seconds = int(os.getenv("BRALE_RISK_REFRESH", "2"))
        self._risk_lock = threading.Lock()
        self._risk_records: Dict[int, Dict[str, float]] = {}
        if os.path.exists(self._risk_db_path):
            threading.Thread(target=self._sync_risk_loop, daemon=True).start()
        else:
            logger.warning("BRALE risk DB 未找到: %s", self._risk_db_path)

    # ---- freqtrade hooks -------------------------------------------------
    def populate_entry_trend(self, dataframe, metadata):
        # 示例策略不自动开仓
        dataframe["enter_long"] = 0
        dataframe["enter_short"] = 0
        return dataframe

    def populate_indicators(self, dataframe, metadata):
        # Brale 侧负责决策，这里无需本地指标计算
        return dataframe

    def custom_stoploss(self, pair, trade, current_time, current_rate, current_profit, **kwargs):
        trade_id = getattr(trade, "id", None)
        logger.info("[brale-shared] custom_stoploss call trade=%s price=%.4f", trade_id, current_rate)
        rec = self._get_risk(trade_id)
        if not rec:
            logger.info("[brale-shared] 未找到 trade=%s 的止损记录", trade_id)
            return None
        if not rec.get("stop_loss"):
            return None
        stop_price = rec["stop_loss"]
        if stop_price <= 0 or trade.open_rate <= 0:
            return None
        if trade.is_short:
            delta = (trade.open_rate - stop_price) / trade.open_rate
        else:
            delta = (stop_price - trade.open_rate) / trade.open_rate
        logger.info(
            "[brale-shared] trade=%s symbol=%s stop=%.4f open=%.4f delta=%.4f",
            getattr(trade, "id", None),
            trade.pair,
            stop_price,
            trade.open_rate,
            delta,
        )
        return delta

    def custom_exit(self, pair, trade, current_time, current_rate, current_profit, **kwargs):
        rec = self._get_risk(getattr(trade, "id", None))
        if not rec:
            return None
        if not rec.get("take_profit"):
            return None
        take_price = rec["take_profit"]
        if take_price <= 0:
            return None
        if (trade.is_short and current_rate <= take_price) or (not trade.is_short and current_rate >= take_price):
            logger.info(
                "[brale-shared] trade=%s symbol=%s 触发 take_profit=%.4f 当前=%.4f",
                getattr(trade, "id", None),
                trade.pair,
                take_price,
                current_rate,
            )
            return "brale_take_profit"
        return None

    def populate_exit_trend(self, dataframe, metadata):
        # 没有本地止盈逻辑，交由 custom_exit 控制
        dataframe["exit_long"] = 0
        dataframe["exit_short"] = 0
        return dataframe

    # ---- risk store sync -------------------------------------------------
    def _sync_risk_loop(self):
        while True:
            try:
                self._load_risk_once()
            except Exception as exc:  # noqa: broad-except
                logger.warning("读取 trade_risk 失败: %s", exc)
            time.sleep(max(1, self._refresh_seconds))

    def _load_risk_once(self):
        if not os.path.exists(self._risk_db_path):
            return
        conn = sqlite3.connect(self._risk_db_path)
        conn.row_factory = sqlite3.Row
        try:
            active_trades = self._active_trade_ids()
            rows = conn.execute(
                "SELECT trade_id, symbol, side, stop_loss, take_profit, updated_at FROM trade_risk"
            ).fetchall()
            next_data: Dict[int, Dict[str, float]] = {}
            if not active_trades:
                self._risk_records = {}
                logger.info("[brale-shared] 当前无持仓，跳过 trade_risk 记录")
                return
            for row in rows:
                if row["trade_id"] not in active_trades:
                    continue
                next_data[row["trade_id"]] = {
                    "symbol": row["symbol"],
                    "side": row["side"],
                    "stop_loss": row["stop_loss"],
                    "take_profit": row["take_profit"],
                    "updated_at": row["updated_at"],
                }
            with self._risk_lock:
                self._risk_records = next_data
            logger.info("[brale-shared] 同步 trade_risk %d 条记录", len(next_data))
        finally:
            conn.close()

    def _get_risk(self, trade_id) -> Optional[Dict[str, float]]:
        if not trade_id:
            return None
        with self._risk_lock:
            return self._risk_records.get(int(trade_id))

    def _active_trade_ids(self):
        try:
            from freqtrade.persistence import Trade

            return {trade.id for trade in Trade.query.filter(Trade.is_open.is_(True))}
        except Exception:
            return set()
