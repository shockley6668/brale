package decision

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"brale/internal/gateway/provider"
	"brale/internal/logger"

	"golang.org/x/sync/errgroup"
)

// Dispatcher manages model invocations, handling timeouts and parallelism.
type Dispatcher struct {
	Providers          []provider.ModelProvider
	Parallel           bool
	TimeoutSeconds     int
	ProviderPreference []string
	FinalDisabled      map[string]bool
}

// NewDispatcher creates a new model dispatcher.
func NewDispatcher(providers []provider.ModelProvider) *Dispatcher {
	return &Dispatcher{
		Providers:      providers,
		Parallel:       true, // Default
		TimeoutSeconds: 120,  // Default safely
	}
}

func (d *Dispatcher) SetParallel(p bool) {
	d.Parallel = p
}

func (d *Dispatcher) SetTimeout(seconds int) {
	d.TimeoutSeconds = seconds
}

func (d *Dispatcher) SetPreference(pref []string) {
	d.ProviderPreference = pref
}

func (d *Dispatcher) SetDisabled(disabled map[string]bool) {
	d.FinalDisabled = disabled
}

// Dispatch sends prompts to available models and collects outputs.
func (d *Dispatcher) Dispatch(ctx context.Context, system, user string, images []provider.ImagePayload) []ModelOutput {
	outs := d.collectOutputs(ctx, func(c context.Context, p provider.ModelProvider) ModelOutput {
		return d.callProvider(c, p, system, user, images)
	})

	if len(d.ProviderPreference) > 0 {
		outs = orderOutputsByPreference(outs, d.ProviderPreference)
	}
	return outs
}

func (d *Dispatcher) collectOutputs(ctx context.Context, call func(context.Context, provider.ModelProvider) ModelOutput) []ModelOutput {
	if !d.Parallel {
		outs := make([]ModelOutput, 0, len(d.Providers))
		for _, p := range d.Providers {
			if p != nil && p.Enabled() {
				if d.isDisabled(p.ID()) {
					continue
				}
				outs = append(outs, call(ctx, p))
			}
		}
		return outs
	}

	enabled := 0
	for _, p := range d.Providers {
		if p != nil && p.Enabled() {
			enabled++
		}
	}
	if enabled == 0 {
		return nil
	}

	outs := make([]ModelOutput, 0, enabled)
	var mu sync.Mutex
	eg, egCtx := errgroup.WithContext(ctx)

	for _, p := range d.Providers {
		if p == nil || !p.Enabled() {
			continue
		}
		if d.isDisabled(p.ID()) {
			continue
		}
		provider := p
		eg.Go(func() error {
			out := d.invokeSafe(egCtx, provider, call)
			mu.Lock()
			outs = append(outs, out)
			mu.Unlock()
			return nil
		})
	}
	_ = eg.Wait()
	return outs
}

func (d *Dispatcher) callProvider(parent context.Context, p provider.ModelProvider, system, user string, baseImages []provider.ImagePayload) ModelOutput {
	cctx := parent
	var cancel context.CancelFunc
	if timeout := d.TimeoutSeconds; timeout > 0 {
		cctx, cancel = context.WithTimeout(parent, time.Duration(timeout)*time.Second)
		defer cancel()
	}

	logger.Debugf("调用模型: %s", p.ID())
	visionEnabled := p.SupportsVision()
	payload := provider.ChatPayload{
		System:     system,
		User:       user,
		ExpectJSON: p.ExpectsJSON(),
	}
	if visionEnabled && len(baseImages) > 0 {
		payload.Images = cloneImages(baseImages)
	}

	purpose := fmt.Sprintf("final decision (images=%d)", len(payload.Images))
	logAIInput("main", p.ID(), purpose, payload.System, payload.User, summarizeImagePayloads(payload.Images))

	start := time.Now()
	raw, err := p.Call(cctx, payload)
	logger.LogLLMResponse("main", p.ID(), purpose, raw)

	if err != nil {
		logger.Warnf("模型 %s 调用失败 elapsed=%s err=%v", p.ID(), time.Since(start).Truncate(time.Millisecond), err)
	}

	// Note: Parsing is handled by Parser component, not here.
	// We return raw output.

	return ModelOutput{
		ProviderID:    p.ID(),
		Raw:           raw,
		Err:           err,
		Images:        cloneImages(payload.Images),
		VisionEnabled: visionEnabled,
		ImageCount:    len(payload.Images),
	}
}

func (d *Dispatcher) invokeSafe(ctx context.Context, p provider.ModelProvider, call func(context.Context, provider.ModelProvider) ModelOutput) (out ModelOutput) {
	defer func() {
		if r := recover(); r != nil {
			logger.Warnf("模型 %s 调用 panic: %v", p.ID(), r)
			out = ModelOutput{
				ProviderID: p.ID(),
				Err:        fmt.Errorf("panic: %v", r),
			}
		}
	}()
	return call(ctx, p)
}

func (d *Dispatcher) isDisabled(id string) bool {
	if len(d.FinalDisabled) == 0 {
		return false
	}
	if strings.TrimSpace(id) == "" {
		return false
	}
	_, ok := d.FinalDisabled[strings.TrimSpace(id)]
	return ok
}

// Helpers

func cloneImages(src []provider.ImagePayload) []provider.ImagePayload {
	if len(src) == 0 {
		return nil
	}
	dst := make([]provider.ImagePayload, len(src))
	copy(dst, src)
	return dst
}
