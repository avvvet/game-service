package models

import "time"

type GameType struct {
	ID        int64     `json:"id"`         // Primary key
	TypeName  string    `json:"type_name"`  // Name of the game type (e.g., Birr 10 Bingo)
	Price     float64   `json:"price"`      // Entry price
	Bonus     float64   `json:"bonus"`      // Bonus prize or amount
	CreatedAt time.Time `json:"created_at"` // Timestamp
	UpdatedAt time.Time `json:"updated_at"` // Timestamp
}
