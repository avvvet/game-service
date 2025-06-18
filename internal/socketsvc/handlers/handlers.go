package handlers

import (
	"encoding/json"
	"net/http"
	"os"

	"github.com/avvvet/bingo-services/internal/comm"
	"github.com/avvvet/bingo-services/internal/socketsvc/ws"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
)

type Handler struct {
	upgrader websocket.Upgrader
	ws       *ws.Ws
}

type Response struct {
	Message string      `json:"message"`
	Code    int         `json:"code"`
	Data    interface{} `json:"data"`
	Error   string      `json:"error"`
}

func NewHandler(s *ws.Ws) *Handler {
	h := &Handler{
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin:     func(r *http.Request) bool { return true },
		},
		ws: s,
	}
	return h
}

// handleWs method receive msg from web clients and sends it to relay service.
func (h *Handler) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Errorf("Failed to upgrade to WebSocket: %v", err)
		http.Error(w, "Failed to upgrade to WebSocket", http.StatusInternalServerError)
		return
	}

	socketId := uuid.New().String()
	h.ws.StoreConnection(socketId, conn)

	log.Infof("New WebSocket connection established: %s", socketId)

	// Handle WebSocket connection
	go h.handleConnection(conn, socketId)
}

func (h *Handler) handleConnection(conn *websocket.Conn, socketId string) {
	// Ensure cleanup happens when connection closes
	defer func() {
		log.Infof("Closing WebSocket connection: %s", socketId)
		conn.Close()
		h.ws.HandleDisconnect(socketId)
	}()

	log.Infof("Started handling connection for socket: %s", socketId)

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			// Check if it's a normal close or unexpected error
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Errorf("WebSocket unexpected close error for socket %s: %v", socketId, err)
			} else {
				log.Infof("WebSocket connection closed normally for socket: %s", socketId)
			}
			break
		}

		// Parse the message
		message := &comm.WSMessage{}
		if err := json.Unmarshal(raw, &message); err != nil {
			log.Errorf("Failed to unmarshal message from socket %s: %v", socketId, err)
			// Send error response back to client
			h.sendErrorToClient(conn, "Invalid message format")
			continue // Don't break, just skip this message
		}

		log.Debugf("Received message from socket %s: type=%s", socketId, message.Type)

		// Handle incoming websocket msg from web client
		h.ws.SocketMessage(socketId, message)
	}
}

// sendErrorToClient sends an error message back to the WebSocket client
func (h *Handler) sendErrorToClient(conn *websocket.Conn, errorMsg string) {
	errorResponse := map[string]interface{}{
		"type":  "error",
		"error": errorMsg,
	}

	if data, err := json.Marshal(errorResponse); err == nil {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Errorf("Failed to send error message to client: %v", err)
		}
	}
}

func (h *Handler) CreateResponse(w http.ResponseWriter, rsp Response) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(rsp.Code)
	if err := json.NewEncoder(w).Encode(rsp); err != nil {
		log.Errorf("Failed to encode response: %v", err)
	}
}

func (h *Handler) HealthHandler(w http.ResponseWriter, r *http.Request) {
	rsp := Response{
		Message: "signal service is running at port " + os.Getenv("SERVICE_PORT"),
		Code:    200,
		Data:    nil,
	}
	if err := json.NewEncoder(w).Encode(rsp); err != nil {
		log.Errorf("Failed to encode health response: %v", err)
	}
}
