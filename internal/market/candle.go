package market

type Candle struct {
	OpenTime        int64   `json:"open_time"`
	CloseTime       int64   `json:"close_time"`
	Open            float64 `json:"open"`
	High            float64 `json:"high"`
	Low             float64 `json:"low"`
	Close           float64 `json:"close"`
	Volume          float64 `json:"volume"`
	TakerBuyVolume  float64 `json:"taker_buy_volume"`
	TakerSellVolume float64 `json:"taker_sell_volume"`
	Trades          int64   `json:"trades"`
}
