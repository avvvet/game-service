package broker

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/avvvet/bingo-services/internal/comm"
	"github.com/gorilla/websocket"
	"github.com/nats-io/nats.go"
	log "github.com/sirupsen/logrus"
)

type Broker struct {
	Conn           *nats.Conn
	GetConnection  func(string) (*websocket.Conn, bool)
	GetRoomSockets func(string) ([]string, bool)

	GameRooms map[int][]string // Gtype -> slice of clients
	Mu        sync.RWMutex
}

func NewBroker(conn *nats.Conn, fncGetConnection func(string) (*websocket.Conn, bool), fncGetRoomSockets func(string) ([]string, bool)) *Broker {
	return &Broker{
		Conn:           conn,
		GetConnection:  fncGetConnection,
		GetRoomSockets: fncGetRoomSockets,

		GameRooms: make(map[int][]string),
	}
}

func (b *Broker) QueueSubscribe(topic, queueGroup string) (*nats.Subscription, error) {
	sub, err := b.Conn.QueueSubscribe(topic, queueGroup, b.handleMessages)
	if err != nil {
		return nil, err
	}

	return sub, nil
}

func (b *Broker) Subscribe(topic string) (*nats.Subscription, error) {
	sub, err := b.Conn.Subscribe(topic, b.handleMessages)
	if err != nil {
		return nil, err
	}

	return sub, nil
}

// publish message
func (b *Broker) Publish(topic string, payload []byte) error {
	err := b.Conn.Publish(topic, payload)
	if err != nil {
		log.Errorf("Error publishing to topic %s: %s", topic, err)
		return err
	}

	return nil
}

// handleMessages receive message from game service
func (b *Broker) handleMessages(msgNats *nats.Msg) {
	message := &comm.WSMessage{}
	err := json.Unmarshal(msgNats.Data, &message)
	if err != nil {
		log.Errorf("Error %s", err)
	}

	switch message.Type {
	case "init-response", "get-wait-game-response":
		b.sendMessage(message)
	case "player-select-card-response":
		b.sendMessage(message)
	case "player-cancel-card-response", "insufficient-balance-response", "check-active-game-response", "bingo-claim-rejected":
		b.sendMessage(message)
	case "deposite-res", "withdrawal-res", "transfer-res", "user-search-res":
		b.sendMessage(message)
	case "balance-resp":
		b.sendMessage(message)
	case "game-started", "bingo-call":
		b.sendMessageGroup(message)
	case "get-wait-game-response-broadcast":
		b.sendMessageGroup(message)
	case "game-finished":
		b.sendMessageWin(message)
	default:
		log.Error("Unknown message")
		return
	}
}

// send socket message to the web client
func (b *Broker) sendMessage(m *comm.WSMessage) {
	socketId := m.SocketId
	if conn, ok := b.GetConnection(socketId); ok {
		if err := conn.WriteJSON(m); err != nil {
			log.Println(err)
		}
	}
}

// send message to group
func (b *Broker) sendMessageGroup(m *comm.WSMessage) {
	var payload struct {
		Gid     int   `json:"game_id"`
		Gtype   int   `json:"gtype"`
		Gnum    int   `json:"game_no"`
		Number  int   `json:"number"`
		History []int `json:"history"`
	}

	if err := json.Unmarshal(m.Data, &payload); err != nil {
		fmt.Println("error-----")
	}

	s, exists := b.GameRooms[payload.Gtype]
	if exists {
		for _, socketId := range s {
			if conn, ok := b.GetConnection(socketId); ok {
				if err := conn.WriteJSON(m); err != nil {
					log.Println(err)
				}
			}
		}
	}
}

func (b *Broker) sendMessageWin(m *comm.WSMessage) {
	var payload struct {
		Gid      int    `json:"game_id"`
		Gtype    int    `json:"gtype"`
		PlayerId int64  `json:"player_id"`
		Name     string `json:"name"`
		Avatar   string `json:"avatar"`
		Marks    []int  `json:"marks"` // winer bingo card, it shows how it win
	}

	if err := json.Unmarshal(m.Data, &payload); err != nil {
		fmt.Println("error-----")
	}

	s, exists := b.GameRooms[payload.Gtype]
	if exists {
		for _, socketId := range s {
			if conn, ok := b.GetConnection(socketId); ok {
				if err := conn.WriteJSON(m); err != nil {
					log.Println(err)
				}
			}
		}
	}
}
