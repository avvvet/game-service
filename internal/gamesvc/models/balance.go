package models

import (
	"time"

	"github.com/shopspring/decimal"
)

type Balance struct {
	ID        int64           `json:"id"`
	UserID    int64           `json:"user_id"`
	TType     string          `json:"ttype"`
	Dr        decimal.Decimal `json:"dr"`
	Cr        decimal.Decimal `json:"cr"`
	TRef      string          `json:"tref"`
	Status    string          `json:"status"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}
