import json
import logging
import os
import sqlite3
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, HTTPServer
from typing import Any, Dict

logging.basicConfig(level=logging.INFO, format="[risk-server] %(message)s")
logger = logging.getLogger(__name__)

DB_PATH = os.environ.get("BRALE_RISK_DB", "/freqtrade/shared/trade_risk.db")
HOST = os.environ.get("RISK_SERVER_HOST", "0.0.0.0")
PORT = int(os.environ.get("RISK_SERVER_PORT", "9000"))
SECRET = os.environ.get("RISK_SERVER_SECRET", "").strip()


def ensure_schema():
    os.makedirs(os.path.dirname(DB_PATH), exist_ok=True)
    conn = sqlite3.connect(DB_PATH)
    try:
        conn.execute(
            """
        CREATE TABLE IF NOT EXISTS trade_risk (
            trade_id INTEGER PRIMARY KEY,
            symbol TEXT,
            pair TEXT,
            side TEXT,
            entry_price REAL,
            stake_amount REAL,
            amount REAL,
            leverage REAL,
            stop_loss REAL,
            take_profit REAL,
            reason TEXT,
            source TEXT,
            status TEXT,
            updated_at INTEGER NOT NULL
        );
        """
        )
        conn.commit()
    finally:
        conn.close()


def upsert(payload: Dict[str, Any], key: str):
    conn = sqlite3.connect(DB_PATH)
    try:
        now = payload.get("timestamp") or 0
        conn.execute(
            """
            INSERT INTO trade_risk(trade_id, symbol, pair, side, entry_price, stake_amount,
                                   amount, leverage, stop_loss, take_profit, reason, source, status, updated_at)
            VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            ON CONFLICT(trade_id) DO UPDATE SET
                stop_loss=excluded.stop_loss,
                take_profit=excluded.take_profit,
                reason=excluded.reason,
                source=excluded.source,
                status=excluded.status,
                updated_at=excluded.updated_at;
            """,
            (
                payload.get("trade_id"),
                payload.get("symbol"),
                payload.get("pair"),
                payload.get("side"),
                payload.get("entry_price"),
                payload.get("stake_amount"),
                payload.get("amount"),
                payload.get("leverage"),
                payload.get("stop_loss"),
                payload.get("take_profit"),
                payload.get("reason"),
                key,
                payload.get("status", "received"),
                now,
            ),
        )
        conn.commit()
    finally:
        conn.close()


class RiskHandler(BaseHTTPRequestHandler):
    def _read_json(self):
        length = int(self.headers.get("content-length", "0"))
        data = self.rfile.read(length)
        try:
            return json.loads(data.decode("utf-8"))
        except json.JSONDecodeError as exc:
            raise ValueError(f"invalid json: {exc}") from exc

    def _require_secret(self):
        if not SECRET:
            return True
        provided = self.headers.get("X-Webhook-Secret", "")
        return provided == SECRET

    def do_POST(self):
        if not self._require_secret():
            self.send_error(HTTPStatus.FORBIDDEN, "invalid secret")
            return
        try:
            payload = self._read_json()
        except ValueError as exc:
            self.send_error(HTTPStatus.BAD_REQUEST, str(exc))
            return
        path = self.path.rstrip("/")
        key = ""
        if path.endswith("adjust-stoploss") or path.endswith("stop_loss"):
            key = "adjust_stop_loss"
        elif path.endswith("adjust-takeprofit") or path.endswith("take_profit"):
            key = "adjust_take_profit"
        else:
            self.send_error(HTTPStatus.NOT_FOUND, "unknown path")
            return
        if not payload.get("trade_id"):
            self.send_error(HTTPStatus.BAD_REQUEST, "trade_id required")
            return
        upsert(payload, key)
        logger.info("stored %s trade_id=%s", key, payload.get("trade_id"))
        self.send_response(HTTPStatus.OK)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps({"status": "ok"}).encode("utf-8"))

    def log_message(self, fmt, *args):  # noqa: D401
        logger.info(fmt, *args)


def main():
    ensure_schema()
    server = HTTPServer((HOST, PORT), RiskHandler)
    logger.info("risk server listening on %s:%s db=%s", HOST, PORT, DB_PATH)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass


if __name__ == "__main__":
    main()
