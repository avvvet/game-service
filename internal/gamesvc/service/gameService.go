package service

import (
	"context"

	"github.com/avvvet/bingo-services/internal/gamesvc/models"
	"github.com/avvvet/bingo-services/internal/gamesvc/store"
)

type GameService struct {
	gameStore *store.GameStore
}

func NewGameService(gameStore *store.GameStore) *GameService {
	return &GameService{gameStore: gameStore}
}

// GetGame retrieves a game based on the game type ID and status
func (s *GameService) GetGameByTypeAndStatus(ctx context.Context, gameTypeID int, status string) (*models.Game, error) {
	// Fetch game from the store
	game, err := s.gameStore.GetGameByTypeAndStatus(ctx, gameTypeID, status)
	if err != nil {
		return nil, err
	}

	return game, nil
}
