package ws

import (
	"encoding/json"
	"fmt"
	"strings"
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

// HandleDisconnect cleans up all resources associated with a disconnected socket
func (s *Ws) HandleDisconnect(socketId string) {
	log.Infof("Cleaning up resources for disconnected socket: %s", socketId)

	// Remove from connection map
	s.connMap.Delete(socketId)

	// Remove from room map
	s.roomMap.Delete(socketId)

	// Remove from all game rooms
	s.Broker.Mu.Lock()
	for gameType, sockets := range s.Broker.GameRooms {
		s.Broker.GameRooms[gameType] = removeSocket(sockets, socketId)
	}
	s.Broker.Mu.Unlock()

	log.Infof("Successfully cleaned up resources for socket: %s", socketId)
}

// helper function to remove a socket from a slice
func removeSocket(sockets []string, socketId string) []string {
	for i, s := range sockets {
		if s == socketId {
			return append(sockets[:i], sockets[i+1:]...)
		}
	}
	return sockets
}

// handle socket message from web clients
func (s *Ws) SocketMessage(socketId string, message *comm.WSMessage) {
	// Validate socket exists
	if _, exists := s.GetConnection(socketId); !exists {
		log.Warnf("Message received for non-existent socket: %s", socketId)
		return
	}

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
	case "transfer":
		s.transfer(socketId, message)
	case "search-user":
		s.searchUser(socketId, message)
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
		log.Errorf("Error: invalid getBalance payload %s", err)
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
		UserID        int64  `json:"userId"`
		Reference     string `json:"referenceNumber"`
		PaymentMethod string `json:"paymentMethod"`
	}

	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		log.Errorf("Error: invalid deposite payload %s", err)
		return
	}

	if payload.UserID == 0 || payload.Reference == "" {
		log.Error("Invalid deposite payload: missing required fields")
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

	log.Infof("Published payment.service message for user %d to topic %s", payload.UserID, topic)
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
		log.Error("Invalid withdrawal payload: missing required fields")
		return
	}

	// Validate account type
	validAccountTypes := map[string]bool{
		"cbe":       true,
		"abyssinia": true,
		"telebirr":  true,
	}
	if !validAccountTypes[payload.AccountType] {
		log.Error("Invalid withdrawal payload: invalid account type")
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
	log.Infof("Published payment.service withdrawal message for user %d (amount: %.2f, account: %s) to topic %s",
		payload.UserID, payload.Amount, payload.AccountType, topic)
}

// Transfer socket handler
func (s *Ws) transfer(socketId string, msg *comm.WSMessage) {
	var payload struct {
		FromUserID string  `json:"fromUserId"`
		ToUserID   string  `json:"toUserId"`
		Amount     float64 `json:"amount"`
	}

	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		log.Errorf("Error: invalid transfer payload %s", err)
		return
	}

	if payload.FromUserID == "" || payload.ToUserID == "" || payload.Amount <= 0 {
		log.Error("Invalid transfer payload: missing required fields")
		return
	}

	// Prevent self-transfer
	if payload.FromUserID == payload.ToUserID {
		log.Error("Invalid transfer payload: cannot transfer to self")
		return
	}

	// Set minimum transfer amount (optional)
	if payload.Amount < 1.0 {
		log.Error("Invalid transfer payload: amount too small")
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

	log.Infof("Published payment.service transfer message from user %s to user %s (amount: %.2f) to topic %s",
		payload.FromUserID, payload.ToUserID, payload.Amount, topic)
}

func (s *Ws) searchUser(socketId string, msg *comm.WSMessage) {
	var payload struct {
		Query string `json:"query"` // Single field for name or ID
	}

	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		log.Errorf("Error: invalid search user payload %s", err)
		return
	}

	if payload.Query == "" {
		log.Error("Invalid search user payload: missing search query")
		return
	}

	// Basic validation
	query := strings.TrimSpace(payload.Query)
	if len(query) < 1 {
		log.Error("Invalid search user payload: query too short")
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

	log.Infof("Published payment.service search-user message for query '%s' to topic %s",
		query, topic)
}

func (s *Ws) claimBingo(socketId string, msg *comm.WSMessage) {
	var payload struct {
		GameID int   `json:"gameId"`
		UserID int64 `json:"userId"`
		Marks  []int `json:"marks"`
	}

	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		log.Errorf("Error: invalid claimBingo payload %s", err)
		return
	}

	if payload.UserID == 0 || payload.GameID == 0 {
		log.Error("Invalid claimBingo payload: missing required fields")
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

	log.Infof("Published claimBingo message for user %d to topic %s", payload.UserID, topic)
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
		log.Error("Invalid selectCard payload: missing required user fields")
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

	log.Infof("Published selectCard message for user %d to topic %s", payload.UserId, topic)
}

func (s *Ws) getWaitGame(socketId string, msg *comm.WSMessage) {
	var payload struct {
		UserId int64 `json:"user_id"`
		Gtype  int   `json:"gtype"`
		Gid    int   `json:"game_id"`
	}

	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		log.Errorf("Error: invalid getWaitGame payload %s", err)
		return
	}

	if msg.Type != "get-wait-game" {
		log.Errorf("Invalid message type for getWaitGame handler: %s", msg.Type)
		return
	}

	if payload.UserId == 0 {
		log.Error("Invalid getWaitGame payload: missing required user fields")
		return
	}

	// for socket grouping per game id
	s.Broker.Mu.Lock()
	// Check if socket is already in this game type room to prevent duplicates
	if !contains(s.Broker.GameRooms[payload.Gtype], socketId) {
		s.Broker.GameRooms[payload.Gtype] = append(s.Broker.GameRooms[payload.Gtype], socketId)
	}
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

	log.Infof("Published getWaitGame message for user %d to topic %s", payload.UserId, topic)
}

func (s *Ws) checkActiveGame(socketId string, msg *comm.WSMessage) {
	var payload struct {
		UserId int64 `json:"user_id"`
	}

	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		log.Errorf("Error: invalid checkActiveGame payload %s", err)
		return
	}

	if msg.Type != "check-active-game" {
		log.Errorf("Invalid message type for checkActiveGame handler: %s", msg.Type)
		return
	}

	if payload.UserId == 0 {
		log.Error("Invalid checkActiveGame payload: missing required user fields")
		return
	}

	// for socket grouping per game type
	staticGameTypes := []int{10, 20, 40, 50, 100, 200}

	// Check if socket is already in any game room to prevent duplicates
	alreadyInRooms := false
	s.Broker.Mu.Lock()
	for _, gtype := range staticGameTypes {
		if contains(s.Broker.GameRooms[gtype], socketId) {
			alreadyInRooms = true
			break
		}
	}
	if !alreadyInRooms {
		for _, gtype := range staticGameTypes {
			s.Broker.GameRooms[gtype] = append(s.Broker.GameRooms[gtype], socketId)
		}
	}
	s.Broker.Mu.Unlock()

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

	log.Infof("Published checkActiveGame message for user %d to topic %s", payload.UserId, topic)
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
		log.Errorf("Error: invalid handleInit payload %s", err)
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
	log.Infof("Stored connection for socket: %s", socketId)
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
