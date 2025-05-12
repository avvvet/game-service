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

// handleWs method recieve msg from web clients and sends it to relay service.
func (h *Handler) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, "Failed to upgrade to WebSocket", http.StatusInternalServerError)
		return
	}

	socketId := uuid.New().String()
	h.ws.StoreConnection(socketId, conn)

	// Handle WebSocket connection
	go h.handleConnection(conn, socketId)
}

func (h *Handler) handleConnection(conn *websocket.Conn, socketId string) {
	defer conn.Close()

	message := &comm.WSMessage{}
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			log.Println(err)
			break
		} else if err := json.Unmarshal(raw, &message); err != nil {
			log.Println(err)
			return
		}

		// handle incoming websocket msg from web client
		h.ws.SocketMessage(socketId, message)
	}
}

func (h *Handler) CreateResponse(w http.ResponseWriter, rsp Response) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(rsp.Code)

	json.NewEncoder(w).Encode(rsp)
}

func (h *Handler) HealthHandler(w http.ResponseWriter, r *http.Request) {
	rsp := Response{
		Message: "signal service is running at port " + os.Getenv("SERVICE_PORT"),
		Code:    200,
		Data:    nil,
	}
	json.NewEncoder(w).Encode(rsp)
}
