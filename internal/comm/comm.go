package comm

import (
	"encoding/json"

	"github.com/avvvet/bingo-services/internal/gamesvc/models"
	"github.com/shopspring/decimal"
)

type WSMessage struct {
	Type     string          `json:"type"` // e.g. "init", "select_card"
	Data     json.RawMessage `json:"data"`
	SocketId string          `json:"socketid"`
}

type PlayerData struct {
	Name    string `json:"name"`
	UserId  int64  `json:"user_id"`
	Balance string `json:"balance"`
}

type DepositeRes struct {
	Status    string `json:"status"` // sucess, faild
	Timestamp int64  `json:"timestamp"`
	Message   string `json:"message"`
}

type Res struct {
	Status bool `json:"status"` // sucess, faild
}

type BalanceStatus struct {
	Status    bool  `json:"status"`    // available true, insufficient false
	Timestamp int64 `json:"timestamp"` // Unix timestamp in milliseconds
}

type WinData struct {
	Gtype        int     `json:"gtype"`
	Gid          int     `json:"game_id"`
	PlayerId     int64   `json:"player_id"`
	Name         string  `json:"name"`
	Avatar       string  `json:"avatar"`
	Marks        []int   `json:"marks"` // winer bing card, it shows how it win
	CardSN       string  `json:"card_sn"`
	WinnerAmount float64 `json:"winner_amount"`
}

type GameData struct {
	Game    *models.Game         `json:"game"`
	Players []*models.GamePlayer `json:"players"`
	Gtype   int                  `json:"gtype"`
	Gid     int                  `json:"game_id"`
	Card    *GameCard            `json:"card,omitempty"`
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
	UserID        int64  `json:"userId"`
	Reference     string `json:"referenceNumber"`
	PaymentMethod string `json:"paymentMethod"`
}

type WithdrawalRequest struct {
	UserID      int64           `json:"userId"`
	Amount      decimal.Decimal `json:"amount"`
	AccountType string          `json:"accountType"`
	AccountNo   string          `json:"accountNo"`
	Name        string          `json:"name"`
}

type WithdrawalRes struct {
	Status       string          `json:"status"`
	WithdrawalID int64           `json:"withdrawalId,omitempty"`
	Amount       decimal.Decimal `json:"amount,omitempty"`
	AccountType  string          `json:"accountType,omitempty"`
	AccountNo    string          `json:"accountNo,omitempty"`
	Name         string          `json:"name,omitempty"`
	Timestamp    int64           `json:"timestamp"` // Unix timestamp in milliseconds
}

// TransferRequest represents the transfer request payload from socket
type TransferRequest struct {
	FromUserID string  `json:"fromUserId"`
	ToUserID   string  `json:"toUserId"`
	Amount     float64 `json:"amount"`
}

// TransferRes represents the transfer response
type TransferRes struct {
	Status        string  `json:"status"`
	Message       string  `json:"message"`
	TransactionID string  `json:"transactionId,omitempty"`
	Amount        float64 `json:"amount,omitempty"`
	Timestamp     int64   `json:"timestamp"`
}

// UserSearchRequest - unified search with single query field
type UserSearchRequest struct {
	Query string `json:"query"` // Can be name or user ID
}

// UserSearchRes represents the user search response
type UserSearchRes struct {
	Status    string      `json:"status"`
	User      *SearchUser `json:"user,omitempty"`
	Timestamp int64       `json:"timestamp"`
}

// SearchUser represents a user in search results
type SearchUser struct {
	UserID string `json:"userId"`
	Name   string `json:"name"`
}
