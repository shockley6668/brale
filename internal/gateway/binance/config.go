package binance

import (
	"strings"
	"time"
)

type Config struct {
	RESTBaseURL string
	HTTPTimeout time.Duration

	ProxyEnabled bool
	RESTProxyURL string
	WSProxyURL   string
}

func (c *Config) withDefaults() Config {
	out := *c
	out.RESTBaseURL = strings.TrimSpace(out.RESTBaseURL)
	if out.RESTBaseURL == "" {
		out.RESTBaseURL = "https://fapi.binance.com"
	}
	if out.HTTPTimeout <= 0 {
		out.HTTPTimeout = 15 * time.Second
	}
	out.RESTProxyURL = strings.TrimSpace(out.RESTProxyURL)
	out.WSProxyURL = strings.TrimSpace(out.WSProxyURL)
	return out
}
