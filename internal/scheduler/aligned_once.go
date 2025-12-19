package scheduler

import (
	"context"
	"time"

	"brale/internal/logger"
)

type AlignedOnceScheduler struct {
	Name           string
	AlignInterval  time.Duration
	Interval       time.Duration
	Offset         time.Duration
	RunImmediately bool

	ctx   context.Context
	nowFn func() time.Time
}

func NewAlignedOnceScheduler(ctx context.Context, alignInterval, interval, offset time.Duration) *AlignedOnceScheduler {
	if ctx == nil {
		ctx = context.Background()
	}
	return &AlignedOnceScheduler{
		AlignInterval: alignInterval,
		Interval:      interval,
		Offset:        offset,
		ctx:           ctx,
		nowFn:         time.Now,
	}
}

func (s *AlignedOnceScheduler) Start(task func()) {
	if s == nil {
		return
	}
	if task == nil {
		logger.Warnf("AlignedOnceScheduler: task is nil, exit")
		return
	}
	if s.AlignInterval <= 0 {
		logger.Warnf("AlignedOnceScheduler: invalid align_interval=%s, exit", s.AlignInterval)
		return
	}
	if s.Interval <= 0 {
		logger.Warnf("AlignedOnceScheduler: invalid interval=%s, exit", s.Interval)
		return
	}
	if s.Offset < 0 {
		logger.Warnf("AlignedOnceScheduler: negative offset=%s, clamp to 0", s.Offset)
		s.Offset = 0
	}
	if s.ctx == nil {
		s.ctx = context.Background()
	}
	if s.nowFn == nil {
		s.nowFn = time.Now
	}

	startAt := s.nowFn().UTC()
	prefix := "AlignedOnceScheduler"
	if s.Name != "" {
		prefix = prefix + "[" + s.Name + "]"
	}
	logger.Infof("%s: started align_interval=%s interval=%s offset=%s run_immediately=%v at=%s",
		prefix, s.AlignInterval, s.Interval, s.Offset, s.RunImmediately, startAt.Format(time.RFC3339))

	if s.RunImmediately {
		logger.Infof("%s: RunImmediately=true, execute once before first alignment", prefix)
		task()
	}

	now := s.nowFn().UTC()
	nextClose := now.Truncate(s.AlignInterval).Add(s.AlignInterval)
	firstAt := nextClose.Add(s.Offset)
	secondAt := firstAt.Add(s.Interval)
	untilClose := nextClose.Sub(now)
	wait := firstAt.Sub(now)
	logger.Infof("%s: init 距离K线收盘=%s (收盘=%s) 第一次执行=%s (in %s) 第二次执行=%s | uptime=%s",
		prefix,
		untilClose.Truncate(time.Second),
		nextClose.Format(time.RFC3339),
		firstAt.Format(time.RFC3339),
		wait.Truncate(time.Second),
		secondAt.Format(time.RFC3339),
		now.Sub(startAt).Truncate(time.Second),
	)

	if !s.waitUntil(firstAt) {
		return
	}
	task()

	anchor := firstAt.UTC()
	nextAt := nextFixedTimeAfter(anchor, s.Interval, s.nowFn().UTC())

	for {
		now := s.nowFn().UTC()
		nextClose := now.Truncate(s.AlignInterval).Add(s.AlignInterval)
		untilClose := nextClose.Sub(now)
		wait := nextAt.Sub(now)
		uptime := now.Sub(startAt)

		logger.Infof("%s: 距离K线收盘=%s (收盘=%s) 下次执行=%s (in %s) | uptime=%s",
			prefix,
			untilClose.Truncate(time.Second),
			nextClose.Format(time.RFC3339),
			nextAt.Format(time.RFC3339),
			wait.Truncate(time.Second),
			uptime.Truncate(time.Second),
		)

		if !s.waitUntil(nextAt) {
			return
		}
		task()
		nextAt = nextFixedTimeAfter(anchor, s.Interval, s.nowFn().UTC())
	}
}

func (s *AlignedOnceScheduler) waitUntil(target time.Time) bool {
	now := s.nowFn().UTC()
	wait := target.Sub(now)
	if wait <= 0 {
		select {
		case <-s.ctx.Done():
			logger.Infof("AlignedOnceScheduler: ctx done, exit")
			return false
		default:
			return true
		}
	}

	timer := time.NewTimer(wait)
	select {
	case <-s.ctx.Done():
		timer.Stop()
		logger.Infof("AlignedOnceScheduler: ctx done, exit")
		return false
	case <-timer.C:
		return true
	}
}

func nextFixedTimeAfter(anchor time.Time, interval time.Duration, now time.Time) time.Time {
	anchor = anchor.UTC()
	now = now.UTC()
	if interval <= 0 {
		return now
	}
	delta := now.Sub(anchor)
	if delta < 0 {
		return anchor
	}
	k := delta / interval
	return anchor.Add((k + 1) * interval)
}
