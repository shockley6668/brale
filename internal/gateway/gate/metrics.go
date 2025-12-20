package gate

import (
	"context"
	"fmt"
	"strings"

	"brale/internal/market"
	symbolpkg "brale/internal/pkg/symbol"

	"github.com/antihax/optional"
	gateapi "github.com/gateio/gateapi-go/v7"
)

var supportedOIPeriods = []string{"5m", "15m", "30m", "1h", "2h", "4h", "6h", "12h", "1d"}

func (s *Source) GetFundingRate(ctx context.Context, sym string) (float64, error) {
	if s == nil || s.rest == nil {
		return 0, fmt.Errorf("gate source not initialized")
	}

	contract := symbolpkg.Gate.ToExchange(sym)
	if strings.TrimSpace(contract) == "" {
		return 0, fmt.Errorf("invalid symbol: %s", sym)
	}
	res, _, err := s.rest.FuturesApi.GetFuturesContract(ctx, gateSettle, contract)
	if err != nil {
		return 0, err
	}
	return parseFloat(res.FundingRate), nil
}

func (s *Source) GetOpenInterestHistory(ctx context.Context, sym, period string, limit int) ([]market.OpenInterestPoint, error) {
	if s == nil || s.rest == nil {
		return nil, fmt.Errorf("gate source not initialized")
	}
	if limit <= 0 {
		limit = 30
	}
	if limit > gateMaxHistoryLimit {
		limit = gateMaxHistoryLimit
	}

	contract := symbolpkg.Gate.ToExchange(sym)
	period = strings.ToLower(strings.TrimSpace(period))
	if contract == "" || period == "" {
		return nil, fmt.Errorf("symbol and period are required")
	}

	opts := &gateapi.ListContractStatsOpts{
		Interval: optional.NewString(period),
		Limit:    optional.NewInt32(int32(limit)),
	}
	stats, _, err := s.rest.FuturesApi.ListContractStats(ctx, gateSettle, contract, opts)
	if err != nil {
		return nil, err
	}

	points := make([]market.OpenInterestPoint, 0, len(stats))
	for _, item := range stats {
		points = append(points, market.OpenInterestPoint{
			Symbol:               contract,
			SumOpenInterest:      float64(item.OpenInterest),
			SumOpenInterestValue: item.OpenInterestUsd,
			Timestamp:            item.Time * 1000,
		})
	}
	return points, nil
}

func (s *Source) SupportedOIPeriods() []string {
	return append([]string(nil), supportedOIPeriods...)
}
