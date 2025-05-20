package store

import (
	"context"
	"fmt"

	"github.com/avvvet/bingo-services/internal/gamesvc/models"

	"github.com/jackc/pgx"
	"github.com/jackc/pgx/v5/pgxpool"
)

type GamePlayerStore struct {
	db *pgxpool.Pool
}

func NewGamePlayerStore(db *pgxpool.Pool) *GamePlayerStore {
	return &GamePlayerStore{db: db}
}

func (s *GamePlayerStore) GetPlayersByGameID(ctx context.Context, gameID int) ([]*models.GamePlayer, error) {
	query := `
		SELECT id, game_id, user_id, card_sn, status, created_at, updated_at
		FROM game_players
		WHERE game_id = $1
	`

	rows, err := s.db.Query(ctx, query, gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var players []*models.GamePlayer
	for rows.Next() {
		var gp models.GamePlayer
		err := rows.Scan(
			&gp.ID,
			&gp.GameID,
			&gp.UserID,
			&gp.CardSN,
			&gp.Status,
			&gp.CreatedAt,
			&gp.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		players = append(players, &gp)
	}

	return players, nil
}

// It fails with an error if:
// - The card is already taken by another player in the same game (unique_game_card constraint).
// - The user has already joined the game (unique_game_user constraint).
// - Any foreign key (game_id, user_id, card_sn) is invalid.
// Returns the created GamePlayer on success, or an error on failure.
func (s *GamePlayerStore) CreateGamePlayerIfAvailable(ctx context.Context, gameID int, userID int64, cardSN string) (*models.GamePlayer, error) {
	// Validate inputs
	if gameID <= 0 {
		return nil, fmt.Errorf("invalid game ID: %d", gameID)
	}
	if userID <= 0 {
		return nil, fmt.Errorf("invalid user ID: %d", userID)
	}
	if cardSN == "" {
		return nil, fmt.Errorf("card serial number cannot be empty")
	}

	// CTE locks the game row and enforces status='waiting'
	const query = `
WITH locked_game AS (
  SELECT id
  FROM games
  WHERE id = $1
    AND status = 'waiting'
  FOR UPDATE
)
INSERT INTO game_players (game_id, user_id, card_sn, status)
SELECT lg.id, $2, $3, 'pending'
FROM locked_game lg
RETURNING id, game_id, user_id, card_sn, status, created_at, updated_at;
`
	gp := &models.GamePlayer{}
	err := s.db.QueryRow(ctx, query, gameID, userID, cardSN).Scan(
		&gp.ID,
		&gp.GameID,
		&gp.UserID,
		&gp.CardSN,
		&gp.Status,
		&gp.CreatedAt,
		&gp.UpdatedAt,
	)
	if err != nil {
		// zero rows means the game isn't waiting (or doesn't exist)
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("cannot join game %d: not in waiting status or not found", gameID)
		}
		// unique constraint violations
		if pgErr, ok := err.(*pgx.PgError); ok && pgErr.Code == "23505" {
			switch pgErr.ConstraintName {
			case "unique_game_card":
				return nil, fmt.Errorf("card %s is already taken for game %d", cardSN, gameID)
			case "unique_game_user":
				return nil, fmt.Errorf("user %d has already joined game %d", userID, gameID)
			}
		}
		// foreign key violations
		if pgErr, ok := err.(*pgx.PgError); ok && pgErr.Code == "23503" {
			return nil, fmt.Errorf("invalid reference: %s", pgErr.Message)
		}
		return nil, fmt.Errorf("failed to create game player: %w", err)
	}

	return gp, nil
}
