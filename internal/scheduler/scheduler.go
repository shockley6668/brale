package scheduler

import (
	"context"
	"time"

	"brale/internal/logger"
)

type AlignedScheduler struct {
	Interval       time.Duration
	Offset         time.Duration
	RunImmediately bool

	ctx   context.Context
	nowFn func() time.Time
}

func NewAlignedScheduler(ctx context.Context, interval, offset time.Duration) *AlignedScheduler {
	if ctx == nil {
		ctx = context.Background()
	}
	return &AlignedScheduler{
		Interval: interval,
		Offset:   offset,
		ctx:      ctx,
		nowFn:    time.Now,
	}
}

func (s *AlignedScheduler) Start(task func()) {
	if s == nil {
		return
	}
	if task == nil {
		logger.Warnf("AlignedScheduler: task is nil, exit")
		return
	}
	if s.Interval <= 0 {
		logger.Warnf("AlignedScheduler: invalid interval=%s, exit", s.Interval)
		return
	}
	if s.Offset < 0 {
		logger.Warnf("AlignedScheduler: negative offset=%s, clamp to 0", s.Offset)
		s.Offset = 0
	}
	if s.ctx == nil {
		s.ctx = context.Background()
	}
	if s.nowFn == nil {
		s.nowFn = time.Now
	}

	startAt := s.nowFn().UTC()
	logger.Infof("AlignedScheduler: started interval=%s offset=%s run_immediately=%v at=%s",
		s.Interval, s.Offset, s.RunImmediately, startAt.Format(time.RFC3339))

	{
		nextClose, wakeAt, untilClose, wait := s.nextTimes(startAt)
		logger.Infof("AlignedScheduler: init 距离K线收盘=%s (收盘=%s) 下一次执行=%s (in %s)",
			untilClose.Truncate(time.Second),
			nextClose.Format(time.RFC3339),
			wakeAt.Format(time.RFC3339),
			wait.Truncate(time.Second),
		)
	}

	if s.RunImmediately {
		logger.Infof("AlignedScheduler: RunImmediately=true, execute once before alignment loop")
		task()
	}

	for {
		now := s.nowFn().UTC()
		nextClose, wakeAt, untilClose, wait := s.nextTimes(now)
		uptime := now.Sub(startAt)

		logger.Infof("AlignedScheduler: 距离K线收盘=%s (收盘=%s) 距离启动执行=%s 将在=%s 执行 下一轮 | uptime=%s",
			untilClose.Truncate(time.Second),
			nextClose.Format(time.RFC3339),
			wait.Truncate(time.Second),
			wakeAt.Format(time.RFC3339),
			uptime.Truncate(time.Second),
		)

		if wait <= 0 {
			task()
			continue
		}

		timer := time.NewTimer(wait)
		select {
		case <-s.ctx.Done():
			timer.Stop()
			logger.Infof("AlignedScheduler: ctx done, exit")
			return
		case <-timer.C:
		}
		task()
	}
}

func (s *AlignedScheduler) nextTimes(now time.Time) (nextClose time.Time, wakeAt time.Time, untilClose time.Duration, wait time.Duration) {
	now = now.UTC()
	nextClose = now.Truncate(s.Interval).Add(s.Interval)
	wakeAt = nextClose.Add(s.Offset)
	untilClose = nextClose.Sub(now)
	wait = wakeAt.Sub(now)
	return nextClose, wakeAt, untilClose, wait
}
