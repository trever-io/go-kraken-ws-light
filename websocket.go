package krakenWebsocketLight

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/aopoltorzhicky/go_kraken/rest"
	"github.com/gorilla/websocket"
)

const KRAKEN_WS_AUTH_URL = "wss://ws-auth.kraken.com"

const SUBSCRIPTION_EVENT = "subscribe"
const HEARTBEAT_EVENT = "heartbeat"
const ERROR_EVENT = "error"

const OPENORDERS_FEED = "openOrders"
const OWNTRADES_FEED = "ownTrades"

const HEARTBEAT_TIMEOUT = 10 * time.Second

type SubscriptionEvent struct {
	Event        string       `json:"event"`
	Subscription Subscription `json:"subscription"`
}

type Subscription struct {
	Name  string `json:"name"`
	Token string `json:"token"`
}

type BasicEvent struct {
	Event       string `json:"event"`
	ErroMessage string `json:"errorMessage"`
}

func NewOpenOrdersSubscriptionEvent(token string) SubscriptionEvent {
	return SubscriptionEvent{
		Event: SUBSCRIPTION_EVENT,
		Subscription: Subscription{
			Name:  OPENORDERS_FEED,
			Token: token,
		},
	}
}

func NewOwnTradesSubscriptionEvent(token string) SubscriptionEvent {
	return SubscriptionEvent{
		Event: SUBSCRIPTION_EVENT,
		Subscription: Subscription{
			Name:  OWNTRADES_FEED,
			Token: token,
		},
	}
}

func (c *Client) doneSafeClose(ch chan interface{}) {
	c.doneCloseOnce.Do(func() {
		close(c.Done)
	})
}

func (c *Client) heartBeatSafeClose(ch chan struct{}) {
	c.heartBeatCloseOnce.Do(func() {
		close(c.heartBeat)
	})
}

type ErrorHandler func(err error)
type OwnTradeHandler func(trade *TradeMessage)
type OpenOrderHandler func(trade *OrderMessage)

type Client struct {
	token                  string
	Done                   chan interface{}
	doneCloseOnce          sync.Once
	heartBeat              chan struct{}
	heartBeatRunning       sync.Once
	heartBeatCloseOnce     sync.Once
	conn                   *websocket.Conn
	ConnectionErrorHandler ErrorHandler
	MessageErrorHandler    ErrorHandler
	KrakenErrorHandler     ErrorHandler

	OwnTradeHandler  OwnTradeHandler
	OpenOrderHandler OpenOrderHandler
}

func NewClient(key, secret string) (*Client, error) {
	api := rest.New(key, secret)
	data, err := api.GetWebSocketsToken()
	if err != nil {
		return nil, err
	}

	done := make(chan interface{})
	heartBeat := make(chan struct{})

	conn, _, err := websocket.DefaultDialer.Dial(KRAKEN_WS_AUTH_URL, nil)
	if err != nil {
		return nil, err
	}

	c := &Client{
		token:     data.Token,
		Done:      done,
		conn:      conn,
		heartBeat: heartBeat,
	}
	go c.receiveMessages()

	return c, nil
}

func (c *Client) Close() {
	c.doneSafeClose(c.Done)
	c.heartBeatSafeClose(c.heartBeat)
	c.conn.Close()
}

func (c *Client) receiveMessages() {
	defer c.doneSafeClose(c.Done)
	for {
		_, msg, err := c.conn.ReadMessage()
		if err != nil {
			if c.ConnectionErrorHandler != nil {
				c.ConnectionErrorHandler(err)
			} else {
				log.Println(err)
			}
			return
		}

		var eventType interface{}
		err = json.Unmarshal(msg, &eventType)
		if err != nil {
			if c.MessageErrorHandler != nil {
				c.MessageErrorHandler(err)
			} else {
				log.Println(err)
			}
			return
		}

		// handle array events
		if ev, ok := eventType.([]interface{}); ok {
			if len(ev) != 3 {
				if c.MessageErrorHandler != nil {
					c.MessageErrorHandler(fmt.Errorf("malformed event"))
				}
				continue
			}

			if feed, ok := ev[1].(string); ok {
				var err error
				switch feed {
				case OPENORDERS_FEED:
					err = c.handleOpenOrdersMessage(ev[0])
				case OWNTRADES_FEED:
					err = c.handleOwnTradesMessage(ev[0])
				default:
					if c.MessageErrorHandler != nil {
						c.MessageErrorHandler(fmt.Errorf("unsupported feed"))
					}
					continue
				}

				if err != nil {
					if c.MessageErrorHandler != nil {
						c.MessageErrorHandler(err)
					}
					return
				}
			} else {
				if c.MessageErrorHandler != nil {
					c.MessageErrorHandler(fmt.Errorf("malformed event"))
				}
				continue
			}

			// handled event
			continue
		}

		// handle struct events
		event := new(BasicEvent)
		err = json.Unmarshal(msg, &event)
		if err != nil {
			if c.MessageErrorHandler != nil {
				c.MessageErrorHandler(err)
			} else {
				log.Println(err)
			}
			return
		}

		// send heartbeat
		if event.Event == HEARTBEAT_EVENT {
			c.heartBeat <- struct{}{}
			continue
		}

		if event.Event == ERROR_EVENT {
			if c.KrakenErrorHandler != nil {
				c.KrakenErrorHandler(fmt.Errorf("error from kraken ws: %v", event.ErroMessage))
			} else {
				log.Println(fmt.Errorf("error from kraken ws: %v", event.ErroMessage))
			}

			return
		}

		log.Println(string(msg))
	}
}

func (c *Client) waitForHeartBeat() {
	defer c.doneSafeClose(c.Done)
	for {
		timer := time.NewTimer(HEARTBEAT_TIMEOUT)
		select {
		case <-c.heartBeat:
			timer.Stop()
		case <-timer.C:
			if c.ConnectionErrorHandler != nil {
				c.ConnectionErrorHandler(fmt.Errorf("websocket timeout"))
			} else {
				log.Println(fmt.Errorf("websocket timeout"))
			}
			return
		}
	}
}

// Topic OpenOrders
func (c *Client) SubscribeOpenOrders() error {
	if c.OpenOrderHandler == nil {
		return fmt.Errorf("no handler setup")
	}

	event := NewOpenOrdersSubscriptionEvent(c.token)
	c.heartBeatRunning.Do(func() {
		go c.waitForHeartBeat()
	})
	return c.conn.WriteJSON(&event)
}

func (c *Client) handleOpenOrdersMessage(data interface{}) error {
	msg := make([]map[string]*OrderMessage, 0)
	// change message back to bytes
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}

	// unmarshal into message
	err = json.Unmarshal(b, &msg)
	if err != nil {
		return err
	}

	for _, msgMap := range msg {
		for id, order := range msgMap {
			order.OrderId = id
			if c.OpenOrderHandler != nil {
				c.OpenOrderHandler(order)
			}
		}
	}

	return nil
}

// Topic OwnTrades
func (c *Client) SubscribeOwnTrades() error {
	if c.OwnTradeHandler == nil {
		return fmt.Errorf("no handler setup")
	}

	event := NewOwnTradesSubscriptionEvent(c.token)
	c.heartBeatRunning.Do(func() {
		go c.waitForHeartBeat()
	})
	return c.conn.WriteJSON(&event)
}

func (c *Client) handleOwnTradesMessage(data interface{}) error {
	msg := make([]map[string]*TradeMessage, 0)
	// change message back to bytes
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}

	// unmarshal into message
	err = json.Unmarshal(b, &msg)
	if err != nil {
		return err
	}

	for _, msgMap := range msg {
		for id, trade := range msgMap {
			trade.TradeId = id
			if c.OwnTradeHandler != nil {
				c.OwnTradeHandler(trade)
			}
		}
	}

	return nil
}
