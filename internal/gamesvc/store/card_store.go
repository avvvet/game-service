package store

import (
	"context"
	"fmt"

	"github.com/avvvet/bingo-services/internal/gamesvc/models"
	"github.com/jackc/pgx/v5/pgxpool"
)

type CardStore struct {
	db *pgxpool.Pool
}

func NewCardStore(db *pgxpool.Pool) *CardStore {
	return &CardStore{db: db}
}

func (s *CardStore) GetCardBySN(ctx context.Context, sn string) (*models.Card, error) {
	query := `
		SELECT id, card_sn, data, created_at, updated_at
		FROM cards
		WHERE card_sn = $1
		LIMIT 1
	`

	var card models.Card
	err := s.db.QueryRow(ctx, query, sn).Scan(
		&card.ID,
		&card.CardSN,
		&card.Data,
		&card.CreatedAt,
		&card.UpdatedAt,
	)

	if err != nil {
		if err.Error() == "no rows in result set" { // fallback for pgxpool
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get card by sn: %w", err)
	}

	return &card, nil
}
