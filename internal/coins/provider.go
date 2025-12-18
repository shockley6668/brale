package coins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// SymbolProvider 币种来源接口
type SymbolProvider interface {
	List(ctx context.Context) ([]string, error)
	Name() string
}

// NormalizeSymbols 标准化币种列表：去重、转大写、添加 USDT 后缀
func NormalizeSymbols(symbols []string) ([]string, error) {
	if len(symbols) == 0 {
		return nil, errors.New("symbol list is empty")
	}
	seen := make(map[string]struct{}, len(symbols))
	out := make([]string, 0, len(symbols))
	for _, s := range symbols {
		s = strings.ToUpper(strings.TrimSpace(s))
		if s == "" {
			continue
		}
		if !strings.HasSuffix(s, "USDT") {
			s += "USDT"
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil, errors.New("symbol list is empty after normalization")
	}
	return out, nil
}

// DefaultSymbolProvider 默认实现：静态列表
type DefaultSymbolProvider struct{ symbols []string }

func NewDefaultProvider(symbols []string) *DefaultSymbolProvider {
	return &DefaultSymbolProvider{symbols: symbols}
}

func (p *DefaultSymbolProvider) Name() string { return "default" }

func (p *DefaultSymbolProvider) List(_ context.Context) ([]string, error) {
	return NormalizeSymbols(p.symbols)
}

// HTTPSymbolProvider 从自定义 API 拉取币种列表
type HTTPSymbolProvider struct {
	URL    string
	Client *http.Client
}

func NewHTTPSymbolProvider(url string) *HTTPSymbolProvider {
	return &HTTPSymbolProvider{URL: url, Client: &http.Client{Timeout: 10 * time.Second}}
}

func (p *HTTPSymbolProvider) Name() string { return "http" }

func (p *HTTPSymbolProvider) List(ctx context.Context) ([]string, error) {
	if p.URL == "" {
		return nil, errors.New("symbol API URL not configured")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := p.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching symbols: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("HTTP status %d", resp.StatusCode)
	}

	// 一次性读取响应体，避免重复请求
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	// 尝试数组格式: ["BTCUSDT", "ETHUSDT"]
	var arr []string
	if err := json.Unmarshal(body, &arr); err == nil {
		return NormalizeSymbols(arr)
	}

	// 尝试对象格式: {"symbols": ["BTCUSDT", "ETHUSDT"]}
	var obj struct {
		Symbols []string `json:"symbols"`
	}
	if err := json.Unmarshal(body, &obj); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}
	return NormalizeSymbols(obj.Symbols)
}
