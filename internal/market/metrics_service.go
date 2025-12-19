package market

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"brale/internal/logger"
)

type DerivativesData struct {
	Symbol      string
	OI          float64
	OIHistory   map[string]float64
	FundingRate float64
	LastUpdate  time.Time
	Error       string
}

type MetricsService struct {
	source  Source
	cache   map[string]DerivativesData
	mu      sync.RWMutex
	symbols []string

	pollInterval           time.Duration
	baseOIHistoryPeriod    string
	oiHistoryLimit         int
	targetOIHistTimeframes []string
}

func (s *MetricsService) GetTargetTimeframes() []string {
	return s.targetOIHistTimeframes
}

func (s *MetricsService) BaseOIHistoryPeriod() string {
	return s.baseOIHistoryPeriod
}

func (s *MetricsService) OIHistoryLimit() int {
	return s.oiHistoryLimit
}

func NewMetricsService(
	source Source,
	symbols []string,
	timeframes []string,
) (*MetricsService, error) {

	validSymbols := make([]string, 0, len(symbols))
	for _, s := range symbols {
		s = strings.ToUpper(strings.TrimSpace(s))
		if s != "" {
			validSymbols = append(validSymbols, s)
		}
	}
	if len(validSymbols) == 0 {
		return nil, nil
	}

	allTimeframes := normalizeTimeframes(timeframes)
	if len(allTimeframes) == 0 {
		return nil, fmt.Errorf("metrics configuration missing timeframes")
	}

	basePeriod, historyLimit, targetTimeframes := calculateOIHistoryParams(allTimeframes)
	if basePeriod == "" {
		return nil, fmt.Errorf("failed to calculate valid OI history params from timeframes: %v", allTimeframes)
	}

	pollInterval := 5 * time.Minute
	if dur, err := parseTimeframe(basePeriod); err == nil && dur > 0 {
		pollInterval = dur
	}

	return &MetricsService{
		source:                 source,
		cache:                  make(map[string]DerivativesData),
		symbols:                validSymbols,
		pollInterval:           pollInterval,
		baseOIHistoryPeriod:    basePeriod,
		oiHistoryLimit:         historyLimit,
		targetOIHistTimeframes: targetTimeframes,
	}, nil
}

func (s *MetricsService) Get(symbol string) (DerivativesData, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, ok := s.cache[strings.ToUpper(strings.TrimSpace(symbol))]
	return data, ok
}

func (s *MetricsService) OI(ctx context.Context, symbol string) (float64, error) {
	data, ok := s.Get(symbol)
	if !ok {
		return 0, fmt.Errorf("MetricsService: %s 的 OI 数据未找到或未更新", symbol)
	}
	if data.Error != "" {
		return 0, fmt.Errorf("MetricsService: %s 的 OI 数据存在错误: %s", symbol, data.Error)
	}
	return data.OI, nil
}

func (s *MetricsService) Funding(ctx context.Context, symbol string) (float64, error) {
	data, ok := s.Get(symbol)
	if !ok {
		return 0, fmt.Errorf("MetricsService: %s 的 Funding 数据未找到或未更新", symbol)
	}
	if data.Error != "" {
		return 0, fmt.Errorf("MetricsService: %s 的 Funding 数据存在错误: %s", symbol, data.Error)
	}
	return data.FundingRate, nil
}

func (s *MetricsService) RefreshSymbol(ctx context.Context, symbol string) {
	if s == nil {
		return
	}
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" || s.source == nil {
		return
	}
	s.updateSymbol(ctx, symbol)
}

func (s *MetricsService) Start(ctx context.Context) {
	count := len(s.symbols)
	if count == 0 {
		logger.Warnf("MetricsService 未配置监控币种，不启动")
		return
	}

	effectiveInterval := s.pollInterval
	if s.pollInterval > 10*time.Second {
		effectiveInterval = s.pollInterval - (s.pollInterval / 10)
		if effectiveInterval < 10*time.Second {
			effectiveInterval = 10 * time.Second
		}
	} else if s.pollInterval == 0 {
		effectiveInterval = 60 * time.Second
	}

	step := effectiveInterval / time.Duration(count)

	minStep := 1 * time.Second
	if step < minStep {
		step = minStep
		logger.Warnf("MetricsService 计算步进 %v 小于最小限制 %v，已调整为 %v。请注意：币种过多可能会导致轮询周期超过决策间隔。", effectiveInterval/time.Duration(count), minStep, step)
	}

	logger.Infof("MetricsService 启动: 监控 %d 个币种, 决策周期 %v, 有效轮询周期 %v, 每个币种步进 %v",
		count, s.pollInterval, effectiveInterval, step)

	ticker := time.NewTicker(step)
	defer ticker.Stop()

	cursor := 0
	for {
		select {
		case <-ctx.Done():
			logger.Infof("MetricsService 收到停止信号，优雅退出")
			return
		case <-ticker.C:

			symbolsCopy := s.symbols

			if len(symbolsCopy) == 0 {
				continue
			}

			sym := symbolsCopy[cursor]

			cursor = (cursor + 1) % len(symbolsCopy)

			updateCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			go func(sCtx context.Context, symbol string) {
				defer cancel()
				s.updateSymbol(sCtx, symbol)
			}(updateCtx, sym)
		}
	}
}

func (s *MetricsService) updateSymbol(ctx context.Context, symbol string) {
	var (
		oiHist  []OpenInterestPoint
		errOI   error
		funding float64
		errFund error
	)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		oiHist, errOI = s.source.GetOpenInterestHistory(ctx, symbol, s.baseOIHistoryPeriod, s.oiHistoryLimit)
		if errOI != nil {
			logger.Warnf("MetricsService: 获取 %s OI 历史失败: %v", symbol, errOI)
		}
	}()

	go func() {
		defer wg.Done()
		funding, errFund = s.source.GetFundingRate(ctx, symbol)
		if errFund != nil {
			logger.Warnf("MetricsService: 获取 %s Funding 失败: %v", symbol, errFund)
		}
	}()

	wg.Wait()

	newData := DerivativesData{
		Symbol:     symbol,
		LastUpdate: time.Now(),
		OIHistory:  make(map[string]float64),
	}

	var allErrors strings.Builder

	if errOI != nil {
		allErrors.WriteString(fmt.Sprintf("获取OI历史失败: %v; ", errOI))
	} else if len(oiHist) == 0 {
		allErrors.WriteString("获取OI历史为空; ")
	} else {

		if len(oiHist) > 0 {
			latestOI := oiHist[len(oiHist)-1].SumOpenInterest
			newData.OI = latestOI
		} else {
			allErrors.WriteString("OI历史数据为空; ")
		}

		for _, tf := range s.targetOIHistTimeframes {
			duration, err := parseTimeframe(tf)
			if err != nil {
				logger.Warnf("MetricsService: 无法解析目标周期 %s: %v", tf, err)
				continue
			}

			targetTime := time.Now().Add(-duration)

			var oiAtPastTime float64
			found := false

			for i := len(oiHist) - 1; i >= 0; i-- {
				point := oiHist[i]
				pointTime := time.UnixMilli(point.Timestamp)

				if pointTime.Before(targetTime) || pointTime.Equal(targetTime) {
					oiAtPastTime = point.SumOpenInterest
					found = true
					break
				}
			}
			if found {
				newData.OIHistory[tf] = oiAtPastTime
			} else {

				newData.OIHistory[tf] = 0
				logger.Debugf("MetricsService: %s 无法找到 %s 周期前的 OI 数据", symbol, tf)
			}
		}
	}

	if errFund != nil {
		allErrors.WriteString(fmt.Sprintf("获取Funding失败: %v", errFund))
	} else {
		newData.FundingRate = funding
	}

	newData.Error = allErrors.String()

	s.mu.Lock()
	s.cache[symbol] = newData
	s.mu.Unlock()

	if newData.Error == "" {
		logger.Debugf("MetricsService: 已更新 %s, OI: %.2f, Funding: %.4f%%", symbol, newData.OI, newData.FundingRate*100)
	} else {
		logger.Warnf("MetricsService: 更新 %s 部分或全部失败: %s", symbol, newData.Error)
	}
}

func normalizeTimeframes(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(items))
	for _, iv := range items {
		norm := strings.ToLower(strings.TrimSpace(iv))
		if norm == "" {
			continue
		}
		set[norm] = struct{}{}
	}
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for tf := range set {
		out = append(out, tf)
	}
	return out
}

func calculateOIHistoryParams(horizonTimeframes []string) (basePeriod string, limit int, targetTimeframes []string) {
	if len(horizonTimeframes) == 0 {
		return "", 0, nil
	}

	minDuration := time.Hour * 24 * 365
	maxDuration := time.Duration(0)

	parsedDurations := make(map[string]time.Duration)
	var allIntervals []string

	for _, tf := range horizonTimeframes {
		d, err := parseTimeframe(tf)
		if err != nil {
			logger.Warnf("无法解析时间周期 %s: %v", tf, err)
			continue
		}
		parsedDurations[tf] = d
		allIntervals = append(allIntervals, tf)

		if d < minDuration {
			minDuration = d
			basePeriod = tf
		}
		if d > maxDuration {
			maxDuration = d
		}
	}

	if basePeriod == "" || minDuration == 0 {
		return "", 0, nil
	}

	binanceOIPeriods := map[string]time.Duration{
		"5m":  5 * time.Minute,
		"15m": 15 * time.Minute,
		"30m": 30 * time.Minute,
		"1h":  1 * time.Hour,
		"2h":  2 * time.Hour,
		"4h":  4 * time.Hour,
		"6h":  6 * time.Hour,
		"12h": 12 * time.Hour,
		"1d":  24 * time.Hour,
	}

	actualBasePeriod := "1h"
	actualBaseDuration := 1 * time.Hour
	for p, d := range binanceOIPeriods {
		if d <= minDuration && d > actualBaseDuration {
			actualBaseDuration = d
			actualBasePeriod = p
		}
	}
	basePeriod = actualBasePeriod

	limit = int(maxDuration/actualBaseDuration) + 5
	if limit < 10 {
		limit = 10
	}
	if limit > 500 {
		limit = 500
	}

	targetTimeframes = allIntervals

	logger.Infof("OI 历史拉取参数计算: BasePeriod=%s, Limit=%d, TargetTimeframes=%v", basePeriod, limit, targetTimeframes)
	return basePeriod, limit, targetTimeframes
}

func parseTimeframe(tf string) (time.Duration, error) {
	if strings.HasSuffix(tf, "m") {
		minutes, err := strconv.Atoi(strings.TrimSuffix(tf, "m"))
		return time.Duration(minutes) * time.Minute, err
	} else if strings.HasSuffix(tf, "h") {
		hours, err := strconv.Atoi(strings.TrimSuffix(tf, "h"))
		return time.Duration(hours) * time.Hour, err
	} else if strings.HasSuffix(tf, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(tf, "d"))
		return time.Duration(days) * 24 * time.Hour, err
	}
	return 0, fmt.Errorf("无法解析时间周期: %s", tf)
}
