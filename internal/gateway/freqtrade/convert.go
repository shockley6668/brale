package freqtrade

import (
	"encoding/json"
	"strings"
	"time"

	"brale/internal/gateway/database"
)

// tradeToLiveRecord converts a Freqtrade Trade to a database LiveOrderRecord.
func tradeToLiveRecord(tr *Trade) database.LiveOrderRecord {
	if tr == nil {
		return database.LiveOrderRecord{}
	}
	now := time.Now()
	// Convert Freqtrade pair (e.g., "ETH/USDT:USDT") to internal symbol (e.g., "ETH/USDT")
	symbol := freqtradePairToSymbol(tr.Pair)
	isOpen := tr.IsOpen
	// /trades 历史接口在不同版本 freqtrade 下可能缺少 is_open 字段（默认 false），
	// 同时部分版本会填充 close_rate（即使尚未平仓）；因此只要 close_date 为空，就视为未平仓兜底。
	if !isOpen {
		closeDate := strings.TrimSpace(tr.CloseDate)
		if closeDate == "" {
			isOpen = true
		}
	}
	rec := database.LiveOrderRecord{
		FreqtradeID: tr.ID,
		Symbol:      symbol,
		Side:        normalizeTradeSide(tr),
		Status:      database.LiveOrderStatusOpen,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if !isOpen {
		rec.Status = database.LiveOrderStatusClosed
	}
	if amt := tr.Amount; amt != 0 {
		rec.Amount = ptrFloat(amt)
		rec.InitialAmount = ptrFloat(amt)
		if !isOpen {
			rec.ClosedAmount = ptrFloat(amt)
		}
	}
	if stake := tr.StakeAmount; stake != 0 {
		rec.StakeAmount = ptrFloat(stake)
		rec.PositionValue = ptrFloat(stake)
	} else if tr.Amount != 0 && tr.OpenRate != 0 {
		rec.PositionValue = ptrFloat(tr.Amount * tr.OpenRate)
	}
	if tr.OpenRate != 0 {
		rec.Price = ptrFloat(tr.OpenRate)
	}
	if tr.Leverage != 0 {
		rec.Leverage = ptrFloat(tr.Leverage)
	}
	if openAt := parseFreqtradeTime(tr.OpenDate); !openAt.IsZero() {
		rec.StartTime = ptrTime(openAt)
		rec.CreatedAt = openAt
	}
	if closeAt := parseFreqtradeTime(tr.CloseDate); !closeAt.IsZero() {
		rec.EndTime = ptrTime(closeAt)
		rec.UpdatedAt = closeAt
		if rec.Status == database.LiveOrderStatusOpen {
			rec.Status = database.LiveOrderStatusClosed
		}
	}
	if isOpen {
		if rate := firstNonZero(tr.CurrentRate, tr.OpenRate); rate != 0 {
			rec.CurrentPrice = ptrFloat(rate)
		}
		if tr.ProfitRatio != 0 {
			rec.CurrentProfitRatio = ptrFloat(tr.ProfitRatio)
			rec.UnrealizedPnLRatio = ptrFloat(tr.ProfitRatio)
		}
		if tr.ProfitAbs != 0 {
			rec.CurrentProfitAbs = ptrFloat(tr.ProfitAbs)
			rec.UnrealizedPnLUSD = ptrFloat(tr.ProfitAbs)
		}
	} else {
		if tr.CloseRate != 0 {
			rec.CurrentPrice = ptrFloat(tr.CloseRate)
		}
		if tr.CloseProfit != 0 {
			rec.PnLRatio = ptrFloat(tr.CloseProfit)
			rec.RealizedPnLRatio = ptrFloat(tr.CloseProfit)
		}
		if tr.CloseProfitAbs != 0 {
			rec.PnLUSD = ptrFloat(tr.CloseProfitAbs)
			rec.RealizedPnLUSD = ptrFloat(tr.CloseProfitAbs)
		}
	}
	rec.LastStatusSync = ptrTime(time.Now())
	if raw, err := json.Marshal(tr); err == nil {
		rec.RawData = string(raw)
	}
	return rec
}

func normalizeTradeSide(tr *Trade) string {
	if tr == nil {
		return "long"
	}
	side := strings.ToLower(strings.TrimSpace(tr.Side))
	if side == "" {
		if tr.IsShort {
			return "short"
		}
		return "long"
	}
	switch {
	case strings.Contains(side, "short"), strings.Contains(side, "sell"):
		return "short"
	default:
		return "long"
	}
}

func ptrTime(t time.Time) *time.Time {
	return &t
}
