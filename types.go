package krakenWebsocketLight

type TradeMessage struct {
	TradeId   string `json:"-"`
	OrderTxId string `json:"ordertxid"`
	PostxId   string `json:"postxid"`
	Pair      string `json:"pair"`
	Time      string `json:"time"`
	Type      string `json:"type"`
	OrderType string `json:"ordertype"`
	Price     string `json:"price"`
	Cost      string `json:"cost"`
	Fee       string `json:"fee"`
	Vol       string `json:"vol"`
	Margin    string `json:"margin"`
	Userref   *int64 `json:"userref"`
}

type OrderMessage struct {
	OrderId string `json:"-"`
	Status  string `json:"status"`
	UserRef *int64 `json:"userref"`
}
