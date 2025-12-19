package scheduler

import (
	"strconv"
	"strings"
	"time"
)

func ParseIntervalDuration(interval string) (time.Duration, bool) {
	interval = strings.ToLower(strings.TrimSpace(interval))
	if interval == "" {
		return 0, false
	}
	unit := interval[len(interval)-1]
	numStr := strings.TrimSpace(interval[:len(interval)-1])
	if numStr == "" {
		return 0, false
	}
	n, err := strconv.Atoi(numStr)
	if err != nil || n <= 0 {
		return 0, false
	}
	switch unit {
	case 'm':
		return time.Duration(n) * time.Minute, true
	case 'h':
		return time.Duration(n) * time.Hour, true
	case 'd':
		return time.Duration(n) * 24 * time.Hour, true
	case 'w':
		return time.Duration(n) * 7 * 24 * time.Hour, true
	default:
		return 0, false
	}
}
