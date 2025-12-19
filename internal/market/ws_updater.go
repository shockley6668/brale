package market

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"brale/internal/logger"
)

type WSUpdater struct {
	Store  KlineStore
	Max    int
	Source Source

	OnConnected    func()
	OnDisconnected func(error)

	OnEvent func(CandleEvent)

	startOnce sync.Once
}

type WSUpdaterOption func(*WSUpdater)

func WithWSCallbacks(onConnect func(), onDisconnect func(error)) WSUpdaterOption {
	return func(u *WSUpdater) {
		u.OnConnected = onConnect
		u.OnDisconnected = onDisconnect
	}
}

func WithWSEventHandler(handler func(CandleEvent)) WSUpdaterOption {
	return func(u *WSUpdater) {
		u.OnEvent = handler
	}
}

func NewWSUpdater(s KlineStore, max int, src Source, opts ...WSUpdaterOption) *WSUpdater {
	u := &WSUpdater{Store: s, Max: max, Source: src}
	for _, opt := range opts {
		if opt != nil {
			opt(u)
		}
	}
	return u
}

func (u *WSUpdater) Update(ctx context.Context, symbol, interval string, k Candle) error {
	return u.Store.Put(ctx, symbol, interval, []Candle{k}, u.Max)
}

func (u *WSUpdater) Start(ctx context.Context, symbols []string, intervals []string) error {
	if u.Source == nil {
		return fmt.Errorf("ws updater missing source")
	}
	if len(symbols) == 0 || len(intervals) == 0 {
		return fmt.Errorf("ws updater requires symbols & intervals")
	}
	opts := SubscribeOptions{
		OnConnect:    u.OnConnected,
		OnDisconnect: u.OnDisconnected,
	}
	events, err := u.Source.Subscribe(ctx, symbols, intervals, opts)
	if err != nil {
		return err
	}
	u.startOnce.Do(func() {
		go u.consume(ctx, events)
	})
	logger.Infof("[WS] 订阅已启动 symbols=%v intervals=%v", symbols, intervals)
	return nil
}

func (u *WSUpdater) consume(ctx context.Context, events <-chan CandleEvent) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-events:
			if !ok {
				return
			}
			candle := evt.Candle
			if err := u.Update(ctx, strings.ToUpper(evt.Symbol), evt.Interval, candle); err != nil {
				logger.Warnf("[WS] 写入 %s %s 失败: %v", evt.Symbol, evt.Interval, err)
			}
			if u.OnEvent != nil {
				u.OnEvent(evt)
			}
		}
	}
}

func (u *WSUpdater) Stats() SourceStats {
	if u.Source == nil {
		return SourceStats{}
	}
	return u.Source.Stats()
}

func (u *WSUpdater) Close() {
	if u.Source != nil {
		if err := u.Source.Close(); err != nil {
			logger.Warnf("[WS] source close error: %v", err)
		}
	}
}
