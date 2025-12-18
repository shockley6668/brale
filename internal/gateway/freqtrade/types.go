package freqtrade

import "time"

func ptrFloat(v float64) *float64 { return &v }

func valOrZero(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

func timeToMillis(t *time.Time) int64 {
	if t == nil || t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}
