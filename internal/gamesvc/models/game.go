package models

import (
	"database/sql"
	"time"
)

type Game struct {
	ID         int64         `json:"id"`           // Primary key
	GameNo     int64         `json:"game_no"`      // Auto-incremented game number (sequence)
	GameTypeID int64         `json:"game_type_id"` // Foreign key to GameTypes
	UserID     sql.NullInt64 `json:"user_id"`      // Foreign key to Users (creator or host)
	TotPrize   float64       `json:"tot_prize"`    // Total prize for the game
	Status     string        `json:"status"`
	CreatedAt  time.Time     `json:"created_at"` // Timestamp
	UpdatedAt  time.Time     `json:"updated_at"` // Timestamp
}
