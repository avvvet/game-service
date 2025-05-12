package handlers

import (
	"encoding/json"
	"net/http"
	"os"

	"github.com/go-chi/jwtauth"
)

type Handler struct {
	tokenAuth *jwtauth.JWTAuth
}

type WSMessage struct {
	Event string `json:"event"`
	Data  string `json:"data"`
}

func NewHandler() *Handler {
	return &Handler{}
}

type Response struct {
	Message string      `json:"message"`
	Code    int         `json:"code"`
	Data    interface{} `json:"data"`
	Error   string      `json:"error"`
}

func (rs *Handler) CreateResponse(w http.ResponseWriter, rsp Response) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(rsp.Code)

	json.NewEncoder(w).Encode(rsp)
}

func (h *Handler) HealthHandler(w http.ResponseWriter, r *http.Request) {
	rsp := Response{
		Message: "relay service is running at port " + os.Getenv("SERVICE_PORT"),
		Code:    200,
		Data:    nil,
	}
	json.NewEncoder(w).Encode(rsp)
}
