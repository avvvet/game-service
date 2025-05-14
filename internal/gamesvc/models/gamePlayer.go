package models

import "time"

type GamePlayer struct {
	ID        int64     `json:"id"`         // Primary key
	GameID    int64     `json:"game_id"`    // FK to games(id)
	UserID    int64     `json:"user_id"`    // FK to users(user_id)
	CardSN    string    `json:"card_sn"`    // FK to cards(sn)
	Status    string    `json:"status"`     // 'pending', 'win', 'loose', 'canceled'
	CreatedAt time.Time `json:"created_at"` // Timestamp
	UpdatedAt time.Time `json:"updated_at"` // Timestamp
}
