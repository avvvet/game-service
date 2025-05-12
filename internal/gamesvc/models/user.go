package models

import (
	"time"
)

// User represents the users table in the database.
type User struct {
	UserId    int64     `json:"user_id"`
	Name      string    `json:"name"`
	Avatar    string    `json:"avatar,omitempty"`
	Email     string    `json:"email,omitempty"`
	Phone     string    `json:"phone,omitempty"`
	Status    string    `json:"status,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
