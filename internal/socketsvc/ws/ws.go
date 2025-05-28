package ws

import (
	"encoding/json"
	"fmt"
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
	case "get-balance":
		s.getBalance(socketId, message)
	case "get-wait-game":
		s.getWaitGame(socketId, message)
	case "player-select-card":
		s.selectCard(socketId, message)
	case "claim-bingo":
		s.claimBingo(socketId, message)
	case "deposite":
		s.deposite(socketId, message)
	default:
		log.Warnf("unknown event received: %s", message.Type)
	}
}

func (s *Ws) getBalance(socketId string, msg *comm.WSMessage) {
	var payload struct {
		UserID int64 `json:"userId"`
	}

	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		log.Errorf("Error: invalid selectCard payload %s", err)
		return
	}

	if payload.UserID == 0 {
		log.Error("Invalid getBalance payload: missing required fields")
		return
	}

	msg.SocketId = socketId

	// Marshal message for NATS
	bytes, err := json.Marshal(msg)
	if err != nil {
		log.Errorf("Failed to marshal WSMessage for NATS: %v", err)
		return
	}

	// Publish and it will be picked by pay service
	topic := "socket.service"
	if err := s.Broker.Publish(topic, bytes); err != nil {
		log.Errorf("Failed to publish to NATS topic %s: %v", topic, err)
		return
	}

	log.Infof("Published getBalance message for user %d to topic %s", payload.UserID, topic)
}

func (s *Ws) deposite(socketId string, msg *comm.WSMessage) {
	var payload struct {
		UserID    int64  `json:"userId"`
		Reference string `json:"referenceNumber"`
	}

	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		log.Errorf("Error: invalid selectCard payload %s", err)
		return
	}

	if payload.UserID == 0 || payload.Reference == "" {
		log.Error("Invalid [deposite] payload: missing required fields")
		return
	}

	msg.SocketId = socketId

	// Marshal message for NATS
	bytes, err := json.Marshal(msg)
	if err != nil {
		log.Errorf("Failed to marshal WSMessage for NATS: %v", err)
		return
	}

	// Publish and it will be picked by pay service
	topic := "payment.request"
	if err := s.Broker.Publish(topic, bytes); err != nil {
		log.Errorf("Failed to publish to NATS topic %s: %v", topic, err)
		return
	}

	log.Infof("Published [payment.request] message for user %d to topic %s", payload.UserID, topic)
}

func (s *Ws) claimBingo(socketId string, msg *comm.WSMessage) {
	var payload struct {
		GameID int   `json:"gameId"`
		UserID int64 `json:"userId"`
		Marks  []int `json:"marks"`
	}

	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		log.Errorf("Error: invalid selectCard payload %s", err)
		return
	}

	if payload.UserID == 0 || payload.GameID == 0 {
		log.Error("Invalid [claimBingo] payload: missing required fields")
		return
	}

	msg.SocketId = socketId

	// Marshal message for NATS
	bytes, err := json.Marshal(msg)
	if err != nil {
		log.Errorf("Failed to marshal WSMessage for NATS: %v", err)
		return
	}

	// Publish and it will be picked by claim service
	topic := "bingo.claim"
	if err := s.Broker.Publish(topic, bytes); err != nil {
		log.Errorf("Failed to publish to NATS topic %s: %v", topic, err)
		return
	}

	log.Infof("Published [claimBingo] message for user %d to topic %s", payload.UserID, topic)
}

func (s *Ws) selectCard(socketId string, msg *comm.WSMessage) {
	var payload struct {
		UserId int64  `json:"user_id"`
		GameId int    `json:"game_id"`
		CardSN string `json:"card_sn"`
		Gtype  int    `json:"gtype"`
	}

	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		log.Errorf("Error: invalid selectCard payload %s", err)
		return
	}

	if msg.Type != "player-select-card" {
		log.Errorf("Invalid message type for [selectCard] handler: %s", msg.Type)
		return
	}

	if payload.UserId == 0 {
		log.Error("Invalid [selectCard] payload: missing required user fields")
		return
	}

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

	log.Infof("Published [selectCard] message for user %d to topic %s", payload.UserId, topic)
}

func (s *Ws) getWaitGame(socketId string, msg *comm.WSMessage) {
	var payload struct {
		UserId int64 `json:"user_id"`
		Gtype  int   `json:"gtype"`
		Gid    int   `json:"game_id"`
	}

	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		log.Errorf("Error: invalid_get_wait_game payload %s", err)
		return
	}

	if msg.Type != "get-wait-game" {
		log.Errorf("Invalid message type for get_wait_game handler: %s", msg.Type)
		return
	}

	if payload.UserId == 0 {
		log.Error("Invalid get_wait_game payload: missing required user fields")
		return
	}

	// for socket grouping per game id
	s.Broker.Mu.Lock()
	s.Broker.GameRooms[payload.Gtype] = append(s.Broker.GameRooms[payload.Gtype], socketId)
	s.Broker.Mu.Unlock()

	so := s.Broker.GameRooms

	for a, b := range so {
		fmt.Printf("game type %d  count socket %d", a, len(b))
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

	log.Infof("Published get_wait_game message for user %d to topic %s", payload.UserId, topic)
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
