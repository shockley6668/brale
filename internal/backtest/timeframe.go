package backtest

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Timeframe 描述回测使用的周期信息（内部 duration + 数据源 interval）
type Timeframe struct {
	Key            string
	Duration       time.Duration
	SourceInterval string
}

var supportedTimeframes = map[string]Timeframe{
	"5m":  {Key: "5m", Duration: 5 * time.Minute, SourceInterval: "5m"},
	"15m": {Key: "15m", Duration: 15 * time.Minute, SourceInterval: "15m"},
	"30m": {Key: "30m", Duration: 30 * time.Minute, SourceInterval: "30m"},
	"1h":  {Key: "1h", Duration: time.Hour, SourceInterval: "1h"},
	"4h":  {Key: "4h", Duration: 4 * time.Hour, SourceInterval: "4h"},
	"1d":  {Key: "1d", Duration: 24 * time.Hour, SourceInterval: "1d"},
	"3d":  {Key: "3d", Duration: 72 * time.Hour, SourceInterval: "3d"},
	"7d":  {Key: "7d", Duration: 7 * 24 * time.Hour, SourceInterval: "1w"},
}

// ParseTimeframe 返回标准化周期定义。
func ParseTimeframe(input string) (Timeframe, error) {
	key := strings.ToLower(strings.TrimSpace(input))
	tf, ok := supportedTimeframes[key]
	if !ok {
		return Timeframe{}, fmt.Errorf("不支持的周期: %s", input)
	}
	return tf, nil
}

// SupportedTimeframes 返回所有支持的 key（排序后）。
func SupportedTimeframes() []string {
	keys := make([]string, 0, len(supportedTimeframes))
	for k := range supportedTimeframes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (tf Timeframe) durationMillis() int64 {
	return tf.Duration.Milliseconds()
}

func alignDown(ts, step int64) int64 {
	if step <= 0 {
		return ts
	}
	rem := ts % step
	if rem < 0 {
		rem += step
	}
	return ts - rem
}

// AlignRange 将输入的毫秒时间对齐到周期网格，保证 start<=end。
func (tf Timeframe) AlignRange(start, end int64) (int64, int64) {
	step := tf.durationMillis()
	if end < start {
		start, end = end, start
	}
	alStart := alignDown(start, step)
	alEnd := alignDown(end, step)
	if alEnd < alStart {
		alEnd = alStart
	}
	return alStart, alEnd
}

// ExpectedCandles 计算 start~end（含）区间应存在的 K 线数量。
func (tf Timeframe) ExpectedCandles(start, end int64) int64 {
	if end < start {
		return 0
	}
	step := tf.durationMillis()
	if step == 0 {
		return 0
	}
	return ((end - start) / step) + 1
}
