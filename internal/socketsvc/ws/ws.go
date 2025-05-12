package ws

import (
	"encoding/json"
	"sync"

	"github.com/avvvet/bingo-services/internal/comm"
	"github.com/avvvet/bingo-services/internal/socketsvc/broker"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
)

type Ws struct {
	connMap sync.Map // to keep track of socket connection with socketId
	roomMap sync.Map // to keep track of roomId with socketId
	Broker  *broker.Broker
}

func NewWs() *Ws {
	return &Ws{}
}

// handle socket message from web clients
func (s *Ws) SocketMessage(socketId string, message *comm.WSMessage) {
	switch message.Type {
	case "init":
		s.handleInit(socketId, message)
	case "offer":

	default:
		log.Warnf("unknown event received: %s", message.Type)
	}
}

func (s *Ws) handleInit(socketId string, msg *comm.WSMessage) {

	var payload struct {
		UserId int64  `json:"user_id"`
		Name   string `json:"name"`
		Phone  string `json:"phone"`
		Email  string `json:"email"`
		Avatar string `json:"avatar"`
	}

	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		log.Errorf("Error: invalid_init_data Malformed init payload %s", err)
		return
	}

	// Validate message type
	if msg.Type != "init" {
		log.Errorf("Invalid message type for init handler: %s", msg.Type)
		return
	}

	// Ensure required fields are present (e.g., UserID, Email)
	if payload.UserId == 0 {
		log.Error("Invalid init payload: missing required user fields")
		return
	}

	// Update message with socket ID
	msg.SocketId = socketId

	// Marshal message for NATS
	bytes, err := json.Marshal(msg)
	if err != nil {
		log.Errorf("Failed to marshal WSMessage for NATS: %v", err)
		return
	}

	// Publish to game service
	topic := "socket.service"
	if err := s.Broker.Publish(topic, bytes); err != nil {
		log.Errorf("Failed to publish to NATS topic %s: %v", topic, err)
		return
	}

	log.Infof("Published init message for user %d to topic %s", payload.UserId, topic)
}

func (s *Ws) StoreConnection(socketId string, conn *websocket.Conn) {
	s.connMap.Store(socketId, conn)
}

func (s *Ws) GetConnection(socketId string) (*websocket.Conn, bool) {
	conn, ok := s.connMap.Load(socketId)
	if !ok {
		return nil, false
	}
	return conn.(*websocket.Conn), true
}

func (s *Ws) StoreRoom(socketId string, roomId string) {
	s.roomMap.Store(socketId, roomId)
}

func (s *Ws) GetRoom(socketId string) (string, bool) {
	room, ok := s.roomMap.Load(socketId)
	if !ok {
		return "", false
	}
	return room.(string), true
}

func (s *Ws) GetRoomSockets(roomId string) ([]string, bool) {
	var sockets []string
	found := false

	s.roomMap.Range(func(key, value interface{}) bool {
		if value.(string) == roomId {
			sockets = append(sockets, key.(string))
			found = true
		}
		return true // continue iterating
	})

	return sockets, found
}

func (s *Ws) GetService(roomId string) string {
	// Check if a relay service is already assigned to the roomId
	relayServiceId, ok := s.Broker.RoomToRelayMap.Load(roomId)
	if !ok {
		// No relay service assigned, so use round-robin to select one
		var selectedRelayServiceId string

		s.Broker.RRmtx.Lock()
		defer s.Broker.RRmtx.Unlock()

		// Get available relay services
		count := 0
		s.Broker.RelayMap.Range(func(key, value any) bool {
			if count == s.Broker.RoundRobinIndex {
				selectedRelayServiceId = key.(string)
				s.Broker.RoundRobinIndex = (s.Broker.RoundRobinIndex + 1) % s.getRelayServiceCount()
				return false // Stop iteration
			}
			count++
			return true // Continue iteration
		})

		// Assign the selected relay service to the roomId
		if selectedRelayServiceId != "" {
			s.Broker.RoomToRelayMap.Store(roomId, selectedRelayServiceId)
			return selectedRelayServiceId
		}

		return "" // No available relay service found
	}

	// Return the existing assigned relay service ID
	return relayServiceId.(string)
}

func (s *Ws) getRelayServiceCount() int {
	count := 0
	s.Broker.RelayMap.Range(func(key, value any) bool {
		count++
		return true
	})
	return count
}
