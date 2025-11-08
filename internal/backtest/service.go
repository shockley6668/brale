package backtest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"brale/internal/logger"
	"brale/internal/market"

	"github.com/google/uuid"
	"golang.org/x/time/rate"
)

// ServiceConfig 配置 FetchService。
type ServiceConfig struct {
	Store           *Store
	Sources         map[string]CandleSource
	DefaultExchange string
	RateLimitPerMin int
	MaxBatch        int
	MaxConcurrent   int
}

// Service 负责管理任务、协调拉取与写库。
type Service struct {
	store           *Store
	sources         map[string]CandleSource
	defaultExchange string
	maxBatch        int

	limiter *rate.Limiter
	sem     chan struct{}

	mu   sync.RWMutex
	jobs map[string]*FetchJob

	baseCtx context.Context
}

func NewService(cfg ServiceConfig) (*Service, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("store 不能为空")
	}
	if len(cfg.Sources) == 0 {
		return nil, fmt.Errorf("至少需要一个数据源")
	}
	ratePerSec := rate.Limit(float64(cfg.RateLimitPerMin) / 60.0)
	if cfg.RateLimitPerMin <= 0 {
		ratePerSec = 8
	}
	maxBatch := cfg.MaxBatch
	if maxBatch <= 0 {
		maxBatch = 1000
	}
	maxConcurrent := cfg.MaxConcurrent
	if maxConcurrent <= 0 {
		maxConcurrent = 2
	}
	svc := &Service{
		store:           cfg.Store,
		sources:         make(map[string]CandleSource),
		defaultExchange: strings.ToLower(cfg.DefaultExchange),
		maxBatch:        maxBatch,
		limiter:         rate.NewLimiter(ratePerSec, maxBatch),
		sem:             make(chan struct{}, maxConcurrent),
		jobs:            make(map[string]*FetchJob),
		baseCtx:         context.Background(),
	}
	for k, v := range cfg.Sources {
		svc.sources[strings.ToLower(k)] = v
	}
	if svc.defaultExchange == "" {
		for k := range svc.sources {
			svc.defaultExchange = k
			break
		}
	}
	return svc, nil
}

// SetContext 注入宿主 ctx，用于任务取消。
func (s *Service) SetContext(ctx context.Context) {
	if ctx != nil {
		s.baseCtx = ctx
	}
}

func (s *Service) ctx() context.Context {
	if s.baseCtx == nil {
		return context.Background()
	}
	return s.baseCtx
}

// SubmitFetch 提交拉取任务；若区间已完整只做一致性检查。
func (s *Service) SubmitFetch(params FetchParams) (FetchJob, error) {
	if params.Symbol == "" {
		return FetchJob{}, fmt.Errorf("symbol 不能为空")
	}
	tf, err := ParseTimeframe(params.Timeframe)
	if err != nil {
		return FetchJob{}, err
	}
	exchange := params.Exchange
	if exchange == "" {
		exchange = s.defaultExchange
	}
	src := s.sources[strings.ToLower(exchange)]
	if src == nil {
		return FetchJob{}, fmt.Errorf("未知数据源: %s", exchange)
	}
	start, end := tf.AlignRange(params.Start, params.End)
	if start == end {
		return FetchJob{}, fmt.Errorf("start 与 end 需要构成区间")
	}
	params.Start = start
	params.End = end

	report, err := s.store.CheckIntegrity(s.ctx(), params.Symbol, params.Timeframe, tf, start, end)
	if err != nil {
		return FetchJob{}, err
	}
	total := report.Expected
	completed := min64(report.Present, total)
	job := &FetchJob{
		ID:        uuid.NewString(),
		Status:    JobStatusPending,
		Params:    params,
		Total:     total,
		Completed: completed,
		StartedAt: time.Now(),
		UpdatedAt: time.Now(),
		Missing:   append([]Gap{}, report.Gaps...),
	}
	s.mu.Lock()
	s.jobs[job.ID] = job
	s.mu.Unlock()
	logger.Infof("[backtest] 任务 %s 提交：%s %s [%d,%d] 预计=%d 缺口=%d", job.ID, params.Symbol, params.Timeframe, params.Start, params.End, total, len(report.Gaps))

	if total == 0 || report.Complete() {
		s.setJobStatus(job.ID, JobStatusDone, "数据已完整，无需重新拉取", report.Gaps)
		return job.copy(), nil
	}

	go s.runJob(job.ID, tf, report, src)
	return job.copy(), nil
}

func (s *Service) runJob(jobID string, tf Timeframe, report IntegrityReport, source CandleSource) {
	select {
	case s.sem <- struct{}{}:
	case <-s.ctx().Done():
		s.setJobStatus(jobID, JobStatusFailed, "服务已关闭", nil)
		return
	}
	defer func() { <-s.sem }()

	job := s.getJob(jobID)
	if job == nil {
		return
	}
	logger.Infof("[backtest] 任务 %s 开始，缺口=%d", jobID, len(report.Gaps))
	s.updateJob(jobID, func(j *FetchJob) {
		j.Status = JobStatusRunning
		j.Message = ""
	})

	params := job.Params
	ctx := s.ctx()
	step := tf.durationMillis()
	var warnings []string

	for _, gap := range report.Gaps {
		cursor := gap.From
		targetEnd := gap.To
		for cursor <= targetEnd {
			if err := ctx.Err(); err != nil {
				s.setJobStatus(jobID, JobStatusFailed, err.Error(), nil)
				return
			}
			if err := s.limiter.Wait(ctx); err != nil {
				s.setJobStatus(jobID, JobStatusFailed, err.Error(), nil)
				return
			}
			remaining := int((targetEnd-cursor)/step) + 1
			if remaining < 1 {
				remaining = 1
			}
			if remaining > s.maxBatch {
				remaining = s.maxBatch
			}
			req := FetchRequest{
				Symbol:   params.Symbol,
				Interval: tf.SourceInterval,
				Start:    cursor,
				End:      targetEnd,
				Limit:    remaining,
			}
			data, err := source.Fetch(ctx, req)
			if err != nil {
				s.setJobStatus(jobID, JobStatusFailed, fmt.Sprintf("%s 拉取失败: %v", source.Name(), err), nil)
				return
			}
			if len(data) == 0 {
				warnings = append(warnings, fmt.Sprintf("区间 [%d,%d] 拉取为空", cursor, targetEnd))
				break
			}
			inserted, err := s.store.InsertCandles(ctx, params.Symbol, params.Timeframe, data)
			if err != nil {
				s.setJobStatus(jobID, JobStatusFailed, fmt.Sprintf("写入失败: %v", err), nil)
				return
			}
			last := data[len(data)-1].OpenTime
			cursor = last + step
			s.updateJob(jobID, func(j *FetchJob) {
				j.Completed += int64(inserted)
				j.UpdatedAt = time.Now()
				if warnings != nil {
					j.Warnings = warnings
				}
			})
			if inserted == 0 {
				break
			}
		}
	}

	finalReport, err := s.store.CheckIntegrity(s.ctx(), params.Symbol, params.Timeframe, tf, params.Start, params.End)
	status := JobStatusDone
	if err != nil {
		status = JobStatusFailed
		warnings = append(warnings, "完整性检查失败: "+err.Error())
	}
	message := "拉取完成"
	if !finalReport.Complete() && status != JobStatusFailed {
		status = JobStatusPartial
		message = "已完成，但仍存在缺口"
	}
	s.updateJob(jobID, func(j *FetchJob) {
		j.Status = status
		j.Message = message
		j.Missing = append([]Gap{}, finalReport.Gaps...)
		j.UpdatedAt = time.Now()
		if len(warnings) > 0 {
			j.Warnings = append([]string{}, warnings...)
		}
	})
	logger.Infof("[backtest] 任务 %s 完成，状态=%s，缺口=%d", jobID, status, len(finalReport.Gaps))
}

func (s *Service) setJobStatus(jobID, status, message string, gaps []Gap) {
	s.updateJob(jobID, func(j *FetchJob) {
		j.Status = status
		j.Message = message
		j.Missing = append([]Gap{}, gaps...)
		j.UpdatedAt = time.Now()
	})
}

func (s *Service) getJob(id string) *FetchJob {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.jobs[id]
}

func (s *Service) updateJob(id string, fn func(*FetchJob)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if job, ok := s.jobs[id]; ok && fn != nil {
		fn(job)
	}
}

// JobSnapshot 返回任务副本。
func (s *Service) JobSnapshot(id string) (FetchJob, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.jobs[id]
	if !ok {
		return FetchJob{}, false
	}
	return job.copy(), true
}

// JobsSnapshot 返回所有任务的拷贝列表。
func (s *Service) JobsSnapshot() []FetchJob {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]FetchJob, 0, len(s.jobs))
	for _, job := range s.jobs {
		out = append(out, job.copy())
	}
	return out
}

// ManifestInfo 读取本地 manifest。
func (s *Service) ManifestInfo(ctx context.Context, symbol, timeframe string) (Manifest, error) {
	if symbol == "" || timeframe == "" {
		return Manifest{}, errors.New("symbol/timeframe 不能为空")
	}
	return s.store.Manifest(ctx, symbol, timeframe)
}

// QueryCandles 读取指定区间 K 线。
func (s *Service) QueryCandles(ctx context.Context, symbol, timeframe string, start, end int64, limit int) ([]market.Candle, error) {
	if symbol == "" || timeframe == "" {
		return nil, errors.New("symbol/timeframe 不能为空")
	}
	return s.store.QueryCandles(ctx, symbol, timeframe, start, end, limit)
}

// AllCandles 返回完整数据集。
func (s *Service) AllCandles(ctx context.Context, symbol, timeframe string) ([]market.Candle, error) {
	if symbol == "" || timeframe == "" {
		return nil, errors.New("symbol/timeframe 不能为空")
	}
	return s.store.ListAllCandles(ctx, symbol, timeframe)
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func min(a int, b int64) int {
	if a < int(b) {
		return a
	}
	return int(b)
}
