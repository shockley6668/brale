package binance

import (
	"context"
	"fmt"
	"strings"

	"brale/internal/market"
	"brale/internal/pkg/symbol"
)

func (s *Source) GetFundingRate(ctx context.Context, sym string) (float64, error) {
	if s == nil || s.client == nil {
		return 0, fmt.Errorf("binance source not initialized")
	}

	binanceSymbol := symbol.Parse(sym).Binance()
	if binanceSymbol == "" {
		return 0, fmt.Errorf("invalid symbol: %s", sym)
	}
	res, err := s.client.NewPremiumIndexService().Symbol(binanceSymbol).Do(ctx)
	if err != nil {
		return 0, err
	}
	for _, entry := range res {
		if entry == nil {
			continue
		}
		if strings.EqualFold(entry.Symbol, binanceSymbol) {
			return parseFloat(entry.LastFundingRate), nil
		}
	}
	if len(res) > 0 {
		return parseFloat(res[0].LastFundingRate), nil
	}
	return 0, fmt.Errorf("funding rate not available for %s", sym)
}

func (s *Source) GetOpenInterestHistory(ctx context.Context, sym, period string, limit int) ([]market.OpenInterestPoint, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("binance source not initialized")
	}
	if limit <= 0 {
		limit = 30
	}
	if limit > 500 {
		limit = 500
	}

	binanceSymbol := symbol.Parse(sym).Binance()
	period = strings.ToLower(strings.TrimSpace(period))
	if binanceSymbol == "" || period == "" {
		return nil, fmt.Errorf("symbol and period are required")
	}
	svc := s.client.NewOpenInterestStatisticsService().Symbol(binanceSymbol).Period(period).Limit(limit)
	stats, err := svc.Do(ctx)
	if err != nil {
		return nil, err
	}
	points := make([]market.OpenInterestPoint, 0, len(stats))
	for _, item := range stats {
		if item == nil {
			continue
		}
		points = append(points, market.OpenInterestPoint{
			Symbol:               item.Symbol,
			SumOpenInterest:      parseFloat(item.SumOpenInterest),
			SumOpenInterestValue: parseFloat(item.SumOpenInterestValue),
			Timestamp:            item.Timestamp,
		})
	}
	return points, nil
}
