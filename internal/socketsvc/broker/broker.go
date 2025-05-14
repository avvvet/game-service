package broker

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/avvvet/bingo-services/internal/comm"
	"github.com/gorilla/websocket"
	"github.com/nats-io/nats.go"
	log "github.com/sirupsen/logrus"
)

type Broker struct {
	Conn           *nats.Conn
	GetConnection  func(string) (*websocket.Conn, bool)
	GetRoomSockets func(string) ([]string, bool)

	RelayMap        sync.Map // keeps available relay services
	RoomToRelayMap  sync.Map // to keep track of roomId with relayId
	RoundRobinIndex int      // Counter for round-robin
	RRmtx           sync.Mutex

	LastHeartbeatMap   sync.Map // Map to store the last heartbeat timestamp
	heartbeatThreshold time.Duration
}

func NewBroker(conn *nats.Conn, fncGetConnection func(string) (*websocket.Conn, bool), fncGetRoomSockets func(string) ([]string, bool)) *Broker {
	return &Broker{
		Conn:               conn,
		GetConnection:      fncGetConnection,
		GetRoomSockets:     fncGetRoomSockets,
		heartbeatThreshold: time.Second * 15,
	}
}

// consume message from relay service
func (b *Broker) QueueSubscribe(topic, queueGroup string) (*nats.Subscription, error) {
	sub, err := b.Conn.QueueSubscribe(topic, queueGroup, b.handleMessages)
	if err != nil {
		return nil, err
	}

	return sub, nil
}

// consume message from relay service
func (b *Broker) Subscribe(topic string) (*nats.Subscription, error) {
	sub, err := b.Conn.Subscribe(topic, b.handleMessages)
	if err != nil {
		return nil, err
	}

	return sub, nil
}

// publish message to relay service
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
