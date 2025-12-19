package gateway

import (
	"fmt"
	"strings"

	brcfg "brale/internal/config"
	"brale/internal/gateway/binance"
	"brale/internal/market"
)

func NewSourceFromConfig(cfg *brcfg.Config) (market.Source, error) {
	if cfg == nil {
		return nil, fmt.Errorf("nil config")
	}
	active := cfg.Market.ResolveActiveSource()
	name := strings.ToLower(active.Name)
	switch name {
	case "", "binance", "binance-futures":
		return binance.New(binance.Config{
			RESTBaseURL:  active.RESTBaseURL,
			ProxyEnabled: active.Proxy.Enabled,
			RESTProxyURL: active.Proxy.RESTURL,
			WSProxyURL:   active.Proxy.WSURL,
		})
	default:
		return nil, fmt.Errorf("unsupported market source: %s", active.Name)
	}
}
