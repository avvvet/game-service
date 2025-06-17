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
	case "check-active-game":
		s.checkActiveGame(socketId, message)
	case "player-select-card", "player-cancel-card":
		s.selectCard(socketId, message)
	case "claim-bingo":
		s.claimBingo(socketId, message)
	case "deposite":
		s.deposite(socketId, message)
	case "withdrawal":
		s.withdrawal(socketId, message)
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
	topic := "payment.service"
	if err := s.Broker.Publish(topic, bytes); err != nil {
		log.Errorf("Failed to publish to NATS topic %s: %v", topic, err)
		return
	}

	log.Infof("Published [payment.service] message for user %d to topic %s", payload.UserID, topic)
}

func (s *Ws) withdrawal(socketId string, msg *comm.WSMessage) {
	var payload struct {
		UserID      int64   `json:"userId"`
		Amount      float64 `json:"amount"`
		AccountType string  `json:"accountType"`
		AccountNo   string  `json:"accountNo"`
		Name        string  `json:"name"`
	}
	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		log.Errorf("Error: invalid withdrawal payload %s", err)
		return
	}

	if payload.UserID == 0 || payload.Amount <= 0 || payload.AccountType == "" || payload.AccountNo == "" || payload.Name == "" {
		log.Error("Invalid [withdrawal] payload: missing required fields")
		return
	}

	// Validate account type
	validAccountTypes := map[string]bool{
		"cbe":       true,
		"abyssinia": true,
		"telebirr":  true,
	}
	if !validAccountTypes[payload.AccountType] {
		log.Error("Invalid [withdrawal] payload: invalid account type")
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
	topic := "payment.service"
	if err := s.Broker.Publish(topic, bytes); err != nil {
		log.Errorf("Failed to publish to NATS topic %s: %v", topic, err)
		return
	}
	log.Infof("Published [payment.service] withdrawal message for user %d (amount: %.2f, account: %s) to topic %s",
		payload.UserID, payload.Amount, payload.AccountType, topic)
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
		fmt.Printf("game type -> %d  count socket %d\n", a, len(b))
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

func (s *Ws) checkActiveGame(socketId string, msg *comm.WSMessage) {
	var payload struct {
		UserId int64 `json:"user_id"`
	}

	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		log.Errorf("Error: invalid_get_wait_game payload %s", err)
		return
	}

	if msg.Type != "check-active-game" {
		log.Errorf("Invalid message type for get_wait_game handler: %s", msg.Type)
		return
	}

	if payload.UserId == 0 {
		log.Error("Invalid get_wait_game payload: missing required user fields")
		return
	}

	// for socket grouping per game type
	staticGameTypes := []int{10, 20, 40, 50, 100, 200}
	if !contains(s.Broker.GameRooms[staticGameTypes[0]], socketId) { // to check if previously this socket to any of the gametype rooms
		s.Broker.Mu.Lock()
		for _, gtype := range staticGameTypes {
			s.Broker.GameRooms[gtype] = append(s.Broker.GameRooms[gtype], socketId)
		}

		s.Broker.Mu.Unlock()
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

	log.Infof("Published get_active_game message for user %d to topic %s", payload.UserId, topic)
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

func contains(slice []string, val string) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}
