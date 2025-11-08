package market

// Candle 是回测写库/读取的统一结构。
type Candle struct {
	OpenTime  int64   `json:"open_time"`
	CloseTime int64   `json:"close_time"`
	Open      float64 `json:"open"`
	High      float64 `json:"high"`
	Low       float64 `json:"low"`
	Close     float64 `json:"close"`
	Volume    float64 `json:"volume"`
	Trades    int64   `json:"trades"`
}
