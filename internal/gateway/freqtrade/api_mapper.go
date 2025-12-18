package freqtrade

import (
	"strconv"
	"time"

	"brale/internal/gateway/database"
	"brale/internal/gateway/exchange"
)

func liveOrderStatusText(status database.LiveOrderStatus) string {
	switch status {
	case database.LiveOrderStatusOpen:
		return "open"
	case database.LiveOrderStatusClosed:
		return "closed"
	case database.LiveOrderStatusPartial:
		return "partial"
	case database.LiveOrderStatusRetrying:
		return "retrying"
	case database.LiveOrderStatusOpening:
		return "opening"
	case database.LiveOrderStatusClosingPartial:
		return "closing_partial"
	case database.LiveOrderStatusClosingFull:
		return "closing_full"
	case database.LiveOrderStatusCanceled:
		return "canceled"
	default:
		return "unknown"
	}
}

func liveOrderToAPIPosition(rec database.LiveOrderRecord, nowMillis int64) exchange.APIPosition {
	openAtMillis := timeToMillis(rec.StartTime)
	closeAtMillis := timeToMillis(rec.EndTime)
	holdingUntil := nowMillis
	if closeAtMillis > 0 {
		holdingUntil = closeAtMillis
	}
	holdingMs := int64(0)
	if openAtMillis > 0 {
		holdingMs = holdingUntil - openAtMillis
		if holdingMs < 0 {
			holdingMs = 0
		}
	}

	currentPrice := valOrZero(rec.CurrentPrice)
	if currentPrice == 0 {
		currentPrice = valOrZero(rec.Price)
	}

	pnlRatio := valOrZero(rec.CurrentProfitRatio)
	pnlUSD := valOrZero(rec.CurrentProfitAbs)
	if rec.Status == database.LiveOrderStatusClosed {
		if v := valOrZero(rec.PnLRatio); v != 0 {
			pnlRatio = v
		}
		if v := valOrZero(rec.PnLUSD); v != 0 {
			pnlUSD = v
		}
	}

	out := exchange.APIPosition{
		TradeID:       rec.FreqtradeID,
		Symbol:        rec.Symbol,
		Side:          rec.Side,
		EntryPrice:    valOrZero(rec.Price),
		Amount:        valOrZero(rec.Amount),
		InitialAmount: valOrZero(rec.InitialAmount),
		Stake:         valOrZero(rec.StakeAmount),
		Leverage:      valOrZero(rec.Leverage),
		PositionValue: valOrZero(rec.PositionValue),
		OpenedAt:      openAtMillis,
		HoldingMs:     holdingMs,
		CurrentPrice:  currentPrice,
		PnLRatio:      pnlRatio,
		PnLUSD:        pnlUSD,
		RealizedPnLRatio: valOrZero(rec.RealizedPnLRatio),
		RealizedPnLUSD:   valOrZero(rec.RealizedPnLUSD),
		UnrealizedPnLRatio: valOrZero(rec.UnrealizedPnLRatio),
		UnrealizedPnLUSD:   valOrZero(rec.UnrealizedPnLUSD),
		Status:            liveOrderStatusText(rec.Status),
	}

	// position_value is displayed as notional (total position value), not margin.
	ref := currentPrice
	if ref <= 0 {
		ref = out.EntryPrice
	}
	if out.Amount > 0 && ref > 0 {
		out.PositionValue = out.Amount * ref
	} else if out.Stake > 0 && out.Leverage > 0 {
		out.PositionValue = out.Stake * out.Leverage
	}

	if rec.Status != database.LiveOrderStatusClosed && out.UnrealizedPnLUSD == 0 && out.UnrealizedPnLRatio == 0 {
		out.UnrealizedPnLUSD = pnlUSD
		out.UnrealizedPnLRatio = pnlRatio
	}

	initAmt := valOrZero(rec.InitialAmount)
	if initAmt > 0 {
		amt := valOrZero(rec.Amount)
		if amt > 0 {
			out.RemainingRatio = amt / initAmt
		} else if rec.ClosedAmount != nil {
			remaining := initAmt - valOrZero(rec.ClosedAmount)
			if remaining < 0 {
				remaining = 0
			}
			out.RemainingRatio = remaining / initAmt
		}
	}

	if closeAtMillis > 0 {
		out.ClosedAt = closeAtMillis
		out.ExitPrice = currentPrice
	}

	return out
}

func exchangePositionToAPIPosition(pos exchange.Position, nowMillis int64) exchange.APIPosition {
	openAtMillis := pos.OpenedAt.UnixMilli()
	closeAtMillis := int64(0)
	holdingUntil := nowMillis
	if !pos.IsOpen && !pos.UpdatedAt.IsZero() {
		closeAtMillis = pos.UpdatedAt.UnixMilli()
		holdingUntil = closeAtMillis
	}
	holdingMs := int64(0)
	if openAtMillis > 0 {
		holdingMs = holdingUntil - openAtMillis
		if holdingMs < 0 {
			holdingMs = 0
		}
	}

	tradeID, _ := strconv.Atoi(pos.ID)
	currentPrice := pos.CurrentPrice
	if currentPrice == 0 {
		currentPrice = pos.EntryPrice
	}

	out := exchange.APIPosition{
		TradeID:            tradeID,
		Symbol:             pos.Symbol,
		Side:               pos.Side,
		EntryPrice:         pos.EntryPrice,
		Amount:             pos.Amount,
		InitialAmount:      pos.InitialAmount,
		Stake:              pos.StakeAmount,
		Leverage:           pos.Leverage,
		OpenedAt:           openAtMillis,
		HoldingMs:          holdingMs,
		StopLoss:           pos.StopLoss,
		TakeProfit:         pos.TakeProfit,
		CurrentPrice:       currentPrice,
		RealizedPnLRatio:   pos.RealizedPnLRatio,
		RealizedPnLUSD:     pos.RealizedPnL,
		UnrealizedPnLRatio: pos.UnrealizedPnLRatio,
		UnrealizedPnLUSD:   pos.UnrealizedPnL,
		Status:             "unknown",
	}
	if pos.IsOpen {
		out.Status = "open"
	} else {
		out.Status = "closed"
		out.ClosedAt = closeAtMillis
		out.ExitPrice = currentPrice
	}

	// Prefer unrealized as main PnL for open positions.
	if pos.IsOpen {
		out.PnLRatio = out.UnrealizedPnLRatio
		out.PnLUSD = out.UnrealizedPnLUSD
	} else {
		out.PnLRatio = out.RealizedPnLRatio
		out.PnLUSD = out.RealizedPnLUSD
	}

	ref := currentPrice
	if ref <= 0 {
		ref = out.EntryPrice
	}
	if out.Amount > 0 && ref > 0 {
		out.PositionValue = out.Amount * ref
	} else if out.Stake > 0 && out.Leverage > 0 {
		out.PositionValue = out.Stake * out.Leverage
	}
	if out.InitialAmount > 0 && out.Amount > 0 {
		out.RemainingRatio = out.Amount / out.InitialAmount
	}
	return out
}

func positionHoldingMs(openedAt time.Time, now time.Time, closedAt time.Time) int64 {
	if openedAt.IsZero() {
		return 0
	}
	end := now
	if !closedAt.IsZero() {
		end = closedAt
	}
	d := end.Sub(openedAt)
	if d < 0 {
		return 0
	}
	return d.Milliseconds()
}
