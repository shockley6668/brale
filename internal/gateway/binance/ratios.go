package binance

import (
	"context"
	"fmt"
	"strings"

	"brale/internal/market"
	"brale/internal/pkg/symbol"
)

func (s *Source) TopPositionRatio(ctx context.Context, sym, period string, limit int) ([]market.LongShortRatioPoint, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("binance source not initialized")
	}
	binanceSymbol := symbol.Parse(sym).Binance()
	period = strings.ToLower(strings.TrimSpace(period))
	if binanceSymbol == "" || period == "" {
		return nil, fmt.Errorf("symbol and period are required")
	}
	if limit <= 0 {
		limit = 30
	}
	if limit > 500 {
		limit = 500
	}
	svc := s.client.NewTopLongShortPositionRatioService().
		Symbol(binanceSymbol).
		Period(period).
		Limit(uint32(limit))
	raw, err := svc.Do(ctx)
	if err != nil {
		return nil, err
	}
	points := make([]market.LongShortRatioPoint, 0, len(raw))
	for _, item := range raw {
		if item == nil {
			continue
		}
		points = append(points, market.LongShortRatioPoint{
			Timestamp: int64(item.Timestamp),
			Ratio:     parseFloat(item.LongShortRatio),
			Long:      parseFloat(item.LongAccount),
			Short:     parseFloat(item.ShortAccount),
		})
	}
	return points, nil
}

func (s *Source) TopAccountRatio(ctx context.Context, sym, period string, limit int) ([]market.LongShortRatioPoint, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("binance source not initialized")
	}
	binanceSymbol := symbol.Parse(sym).Binance()
	period = strings.ToLower(strings.TrimSpace(period))
	if binanceSymbol == "" || period == "" {
		return nil, fmt.Errorf("symbol and period are required")
	}
	if limit <= 0 {
		limit = 30
	}
	if limit > 500 {
		limit = 500
	}
	svc := s.client.NewTopLongShortAccountRatioService().
		Symbol(binanceSymbol).
		Period(period).
		Limit(uint32(limit))
	raw, err := svc.Do(ctx)
	if err != nil {
		return nil, err
	}
	points := make([]market.LongShortRatioPoint, 0, len(raw))
	for _, item := range raw {
		if item == nil {
			continue
		}
		points = append(points, market.LongShortRatioPoint{
			Timestamp: int64(item.Timestamp),
			Ratio:     parseFloat(item.LongShortRatio),
			Long:      parseFloat(item.LongAccount),
			Short:     parseFloat(item.ShortAccount),
		})
	}
	return points, nil
}

func (s *Source) GlobalAccountRatio(ctx context.Context, sym, period string, limit int) ([]market.LongShortRatioPoint, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("binance source not initialized")
	}
	binanceSymbol := symbol.Parse(sym).Binance()
	period = strings.ToLower(strings.TrimSpace(period))
	if binanceSymbol == "" || period == "" {
		return nil, fmt.Errorf("symbol and period are required")
	}
	if limit <= 0 {
		limit = 30
	}
	if limit > 500 {
		limit = 500
	}
	svc := s.client.NewLongShortRatioService().
		Symbol(binanceSymbol).
		Period(period).
		Limit(limit)
	raw, err := svc.Do(ctx)
	if err != nil {
		return nil, err
	}
	points := make([]market.LongShortRatioPoint, 0, len(raw))
	for _, item := range raw {
		if item == nil {
			continue
		}
		points = append(points, market.LongShortRatioPoint{
			Timestamp: int64(item.Timestamp),
			Ratio:     parseFloat(item.LongShortRatio),
			Long:      0,
			Short:     0,
		})
	}
	return points, nil
}
