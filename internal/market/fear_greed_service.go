package market

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"brale/internal/logger"
)

const (
	fearGreedEndpoint       = "https://api.alternative.me/fng/?limit=5"
	fearGreedErrorBackoff   = 2 * time.Minute
	fearGreedFallbackUpdate = 12 * time.Hour
)

type FearGreedPoint struct {
	Value          int
	Classification string
	Timestamp      time.Time
}

type FearGreedData struct {
	Value           int
	Classification  string
	Timestamp       time.Time
	TimeUntilUpdate time.Duration
	History         []FearGreedPoint
	LastUpdate      time.Time
	Error           string
}

type FearGreedService struct {
	endpoint string
	client   *http.Client

	mu         sync.RWMutex
	data       FearGreedData
	nextUpdate time.Time
	refreshMu  sync.Mutex
}

func NewFearGreedService() *FearGreedService {
	return &FearGreedService{
		endpoint: fearGreedEndpoint,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

func (s *FearGreedService) Get() (FearGreedData, bool) {
	if s == nil {
		return FearGreedData{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	ok := !s.data.LastUpdate.IsZero()
	return s.data, ok
}

func (s *FearGreedService) RefreshIfStale(ctx context.Context) {
	if s == nil {
		return
	}
	now := time.Now()
	s.mu.RLock()
	next := s.nextUpdate
	last := s.data.LastUpdate
	s.mu.RUnlock()
	if !last.IsZero() && !next.IsZero() && now.Before(next) {
		return
	}

	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()

	s.mu.RLock()
	next = s.nextUpdate
	last = s.data.LastUpdate
	s.mu.RUnlock()
	if !last.IsZero() && !next.IsZero() && now.Before(next) {
		return
	}
	if err := s.refresh(ctx); err != nil {
		logger.Warnf("Fear & Greed 刷新失败: %v", err)
	}
}

type fearGreedResponse struct {
	Data []struct {
		Value               string `json:"value"`
		ValueClassification string `json:"value_classification"`
		Timestamp           string `json:"timestamp"`
		TimeUntilUpdate     string `json:"time_until_update"`
	} `json:"data"`
	Metadata struct {
		Error interface{} `json:"error"`
	} `json:"metadata"`
}

func (s *FearGreedService) refresh(ctx context.Context) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("fear & greed service not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.endpoint, nil)
	if err != nil {
		s.setError(err)
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		s.setError(err)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		err := fmt.Errorf("unexpected status %s", resp.Status)
		s.setError(err)
		return err
	}

	var payload fearGreedResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		s.setError(err)
		return err
	}
	if payload.Metadata.Error != nil {
		err := fmt.Errorf("api error: %v", payload.Metadata.Error)
		s.setError(err)
		return err
	}
	if len(payload.Data) == 0 {
		err := fmt.Errorf("api data empty")
		s.setError(err)
		return err
	}

	points := make([]FearGreedPoint, 0, len(payload.Data))
	for _, item := range payload.Data {
		value, err := strconv.Atoi(strings.TrimSpace(item.Value))
		if err != nil {
			continue
		}
		var ts time.Time
		if tsRaw := strings.TrimSpace(item.Timestamp); tsRaw != "" {
			if sec, err := strconv.ParseInt(tsRaw, 10, 64); err == nil {
				ts = time.Unix(sec, 0).UTC()
			}
		}
		points = append(points, FearGreedPoint{
			Value:          value,
			Classification: strings.TrimSpace(item.ValueClassification),
			Timestamp:      ts,
		})
	}
	if len(points) == 0 {
		err := fmt.Errorf("api data invalid")
		s.setError(err)
		return err
	}

	var until time.Duration
	item := payload.Data[0]
	if raw := strings.TrimSpace(item.TimeUntilUpdate); raw != "" {
		if secs, err := strconv.ParseInt(raw, 10, 64); err == nil && secs > 0 {
			until = time.Duration(secs) * time.Second
		}
	}
	latest := points[0]

	now := time.Now()
	next := now.Add(fearGreedFallbackUpdate)
	if until > 0 {
		next = now.Add(until)
	}

	data := FearGreedData{
		Value:           latest.Value,
		Classification:  latest.Classification,
		Timestamp:       latest.Timestamp,
		TimeUntilUpdate: until,
		History:         points,
		LastUpdate:      now,
	}
	s.setData(data, next)
	return nil
}

func (s *FearGreedService) setError(err error) {
	if s == nil {
		return
	}
	now := time.Now()
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	data := FearGreedData{
		LastUpdate: now,
		Error:      msg,
	}
	s.setData(data, now.Add(fearGreedErrorBackoff))
}

func (s *FearGreedService) setData(data FearGreedData, next time.Time) {
	s.mu.Lock()
	s.data = data
	s.nextUpdate = next
	s.mu.Unlock()
}
