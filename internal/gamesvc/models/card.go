package models

import "time"

type Card struct {
	ID        int64     `json:"id"`      // Primary key
	CardSN    string    `json:"card_sn"` // Unique serial number
	Data      string    `json:"data"`    // Serialized card data (e.g., JSON or string representation)
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
