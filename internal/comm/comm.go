package comm

import (
	"encoding/json"
	"time"

	"github.com/avvvet/bingo-services/internal/gamesvc/models"
)

type WSMessage struct {
	Type     string          `json:"type"` // e.g. "init", "select_card"
	Data     json.RawMessage `json:"data"`
	SocketId string          `json:"socketid"`
}

type MediaControl struct {
	Media   string `json:"media"` // audio or video
	Enabled bool   `json:"enabled"`
}

type SimulcastLayer struct {
	RID string `json:"rid"` // q, h, f
}

type RelayRegistration struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type ServiceHeartbeat struct {
	ID        string    `json:"id"` // service id
	Timestamp time.Time `json:"timestamp"`
}

type ServiceShutdown struct {
	ID string `json:"id"` // service id
}

type UserData struct {
	UserId        string `json:"userid"`
	UserName      string `json:"username"`
	Audio         bool   `json:"audio"`
	Video         bool   `json:"video"`
	SocketId      string `json:"socketid"`
	IsActiveTrack bool   `json:"activetrack"`
}

type RoomNotification struct {
	RoomId   string     `json:"roomid"`
	SocketId string     `json:"socketid"`
	StreamId string     `json:"streamid"` // this is streamid to be removed from the client UI
	UserData []UserData `json:"users"`    // users in a room
	Type     string     `json:"type"`
	Name     string     `json:"name"` // user name or public name
}

type PlayerData struct {
	Name    string `json:"name"`
	UserId  int64  `json:"user_id"`
	Balance string `json:"balance"`
}

type DepositeRes struct {
	Status string `json:"status"` // sucess, faild
}

type WinData struct {
	Gtype    int    `json:"gtype"`
	Gid      int    `json:"game_id"`
	PlayerId int64  `json:"player_id"`
	Name     string `json:"name"`
	Avatar   string `json:"avatar"`
	Marks    []int  `json:"marks"` // winer bing card, it shows how it win
}

type GameData struct {
	Game    *models.Game         `json:"game"`
	Players []*models.GamePlayer `json:"players"`
	Gtype   int                  `json:"gtype"`
	Gid     int                  `json:"game_id"`
}

type GameCard struct {
	CardSN string `json:"card_sn"` // Unique serial number
	Data   string `json:"data"`
}

type GameType struct {
	Gtype int `json:"gtype"`
	Gid   int `json:"game_id"`
	Gnum  int `json:"game_no"`
}

type CallMessage struct {
	Gtype   int   `json:"gtype"`
	Gid     int   `json:"game_id"`
	Number  int   `json:"number"`
	History []int `json:"history"`
}

// PaymentRequest is the incoming event when a user submits a reference
type PaymentRequest struct {
	UserID    int64  `json:"userId"`
	Reference string `json:"referenceNumber"`
}
