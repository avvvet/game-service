package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/avvvet/bingo-services/internal/gamesvc/models"
	"github.com/jackc/pgx/v5/pgxpool"
)

type GameStore struct {
	db *pgxpool.Pool // Update to use connection pool
}

func NewGameStore(db *pgxpool.Pool) *GameStore {
	return &GameStore{db: db}
}

func (s *GameStore) GetGameByID(ctx context.Context, gameID int) (*models.Game, error) {
	query := `
		SELECT id, game_no, game_type_id, user_id, tot_priz, status, created_at, updated_at
		FROM games
		WHERE id = $1
	`

	game := &models.Game{}
	err := s.db.QueryRow(ctx, query, gameID).Scan(
		&game.ID,
		&game.GameNo,
		&game.GameTypeID,
		&game.UserID,
		&game.TotPrize,
		&game.Status,
		&game.CreatedAt,
		&game.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil // Game not found
		}
		return nil, fmt.Errorf("failed to get game by ID: %w", err)
	}

	return game, nil
}

// GetGameByTypeAndStatus retrieves a game based on the game type ID and status
func (s *GameStore) GetGameByTypeAndStatus(ctx context.Context, gameTypeID int, status string) (*models.Game, error) {
	// SQL query to find a game by game_type_id and status
	query := `
		SELECT id, game_no, game_type_id, user_id, tot_priz, status, created_at, updated_at
		FROM games
		WHERE game_type_id = $1 AND status = $2
		LIMIT 1`

	// Initialize the Game model
	game := &models.Game{}

	// Execute the query and scan the result
	err := s.db.QueryRow(ctx, query, gameTypeID, status).Scan(
		&game.ID,
		&game.GameNo,
		&game.GameTypeID,
		&game.UserID,
		&game.TotPrize,
		&game.Status,
		&game.CreatedAt,
		&game.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil // Return nil, nil to indicate no game found
		}
		return nil, fmt.Errorf("failed to get game: %w", err)
	}

	return game, nil
}
