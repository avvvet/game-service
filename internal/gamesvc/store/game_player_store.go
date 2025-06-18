package store

import (
	"context"
	"database/sql"
	"errors"
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

func (s *GamePlayerStore) GetActiveGameForUser(ctx context.Context, userID int64) (*models.GamePlayer, error) {
	query := `
		SELECT gp.id, gp.game_id, gp.user_id, gp.card_sn, gp.status, gp.created_at, gp.updated_at
		FROM game_players gp
		INNER JOIN games g ON gp.game_id = g.id
		WHERE gp.user_id = $1 AND g.status = 'started'
		LIMIT 1
	`

	var gp models.GamePlayer
	err := s.db.QueryRow(ctx, query, userID).Scan(
		&gp.ID,
		&gp.GameID,
		&gp.UserID,
		&gp.CardSN,
		&gp.Status,
		&gp.CreatedAt,
		&gp.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil // No active game found, not an error
		}
		return nil, fmt.Errorf("failed to query active game for user %d: %w", userID, err)
	}

	return &gp, nil
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

func (s *GamePlayerStore) CreateGamePlayerIfAvailable(ctx context.Context, gameID int, userID int64, cardSN string) (*models.GamePlayer, error) {
	// Start a transaction to ensure data consistency
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// First, check if the game exists and is in "waiting" status
	var gameStatus string
	checkGameQuery := `
		SELECT status
		FROM games
		WHERE id = $1`
	err = tx.QueryRow(ctx, checkGameQuery, gameID).Scan(&gameStatus)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("game with ID %d not found", gameID)
		}
		return nil, fmt.Errorf("failed to check game status: %w", err)
	}

	// Check if game is in waiting status
	if gameStatus != "waiting" {
		return nil, fmt.Errorf("game is not available for joining, current status: %s", gameStatus)
	}

	// Check if user is already in this game
	var existingPlayerID int
	checkUserQuery := `
		SELECT id
		FROM game_players
		WHERE game_id = $1 AND user_id = $2`
	err = tx.QueryRow(ctx, checkUserQuery, gameID, userID).Scan(&existingPlayerID)
	if err == nil {
		// Record found - user is already in the game
		return nil, fmt.Errorf("user %d is already in game %d", userID, gameID)
	} else if !errors.Is(err, sql.ErrNoRows) {
		// Some other error occurred
		return nil, fmt.Errorf("failed to check existing user in game: %w", err)
	}

	// Check if card is already used in this game
	var existingCardPlayerID int
	checkCardQuery := `
		SELECT id
		FROM game_players
		WHERE game_id = $1 AND card_sn = $2`
	err = tx.QueryRow(ctx, checkCardQuery, gameID, cardSN).Scan(&existingCardPlayerID)
	if err == nil {
		// Record found - card is already used in the game
		return nil, fmt.Errorf("card %s is already used in game %d", cardSN, gameID)
	} else if !errors.Is(err, sql.ErrNoRows) {
		// Some other error occurred
		return nil, fmt.Errorf("failed to check existing card in game: %w", err)
	}

	// Create the game player
	insertQuery := `
		INSERT INTO game_players (game_id, user_id, card_sn, status, created_at, updated_at)
		VALUES ($1, $2, $3, 'pending', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		RETURNING id, game_id, user_id, card_sn, status, created_at, updated_at`
	var gamePlayer models.GamePlayer
	err = tx.QueryRow(ctx, insertQuery, gameID, userID, cardSN).Scan(
		&gamePlayer.ID,
		&gamePlayer.GameID,
		&gamePlayer.UserID,
		&gamePlayer.CardSN,
		&gamePlayer.Status,
		&gamePlayer.CreatedAt,
		&gamePlayer.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create game player: %w", err)
	}

	// Update game's updated_at to reset the 30-second timer**
	updateGameQuery := `
		UPDATE games 
		SET updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND status = 'waiting'`
	_, err = tx.Exec(ctx, updateGameQuery, gameID)
	if err != nil {
		return nil, fmt.Errorf("failed to update game timestamp: %w", err)
	}

	// Commit the transaction
	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return &gamePlayer, nil
}

// It fails with an error if:
// - The card is already taken by another player in the same game (unique_game_card constraint).
// - The user has already joined the game (unique_game_user constraint).
// - Any foreign key (game_id, user_id, card_sn) is invalid.
// Returns the created GamePlayer on success, or an error on failure.
func (s *GamePlayerStore) CreateGamePlayerIfAvailableOld(ctx context.Context, gameID int, userID int64, cardSN string) (*models.GamePlayer, error) {
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

func (s *GamePlayerStore) DeleteGamePlayerCard(ctx context.Context, gameID int, userID int64) error {
	// Start a transaction to ensure data consistency
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// First, check if the game exists and is in "waiting" status
	var gameStatus string
	checkGameQuery := `
		SELECT status
		FROM games
		WHERE id = $1`
	err = tx.QueryRow(ctx, checkGameQuery, gameID).Scan(&gameStatus)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("game with ID %d not found", gameID)
		}
		return fmt.Errorf("failed to check game status: %w", err)
	}

	// Check if game is in waiting status (only allow cancellation in waiting status)
	if gameStatus != "waiting" {
		return fmt.Errorf("cannot cancel card selection, game status is: %s", gameStatus)
	}

	// Check if user has a card in this game
	var existingPlayerID int
	var cardSN string
	checkUserQuery := `
		SELECT id, card_sn
		FROM game_players
		WHERE game_id = $1 AND user_id = $2`
	err = tx.QueryRow(ctx, checkUserQuery, gameID, userID).Scan(&existingPlayerID, &cardSN)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("user %d has no card selected in game %d", userID, gameID)
		}
		return fmt.Errorf("failed to check user's card in game: %w", err)
	}

	// Delete the game player record
	deleteQuery := `
		DELETE FROM game_players
		WHERE game_id = $1 AND user_id = $2`
	result, err := tx.Exec(ctx, deleteQuery, gameID, userID)
	if err != nil {
		return fmt.Errorf("failed to delete game player: %w", err)
	}

	// Check if the deletion was successful
	rowsAffected := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("no card found to cancel for user %d in game %d", userID, gameID)
	}

	// Commit the transaction
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}
