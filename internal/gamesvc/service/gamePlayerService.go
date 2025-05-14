package service

import (
	"context"

	"github.com/avvvet/bingo-services/internal/gamesvc/models"
	"github.com/avvvet/bingo-services/internal/gamesvc/store"
)

type GamePlayerService struct {
	store *store.GamePlayerStore
}

func NewGamePlayerService(store *store.GamePlayerStore) *GamePlayerService {
	return &GamePlayerService{store: store}
}

func (s *GamePlayerService) GetGamePlayers(ctx context.Context, gameID int) ([]*models.GamePlayer, error) {
	return s.store.GetPlayersByGameID(ctx, gameID)
}

func (s *GamePlayerService) CreateGamePlayerIfAvailable(ctx context.Context, gameID int, userID int64, cardSN string) (*models.GamePlayer, error) {
	return s.store.CreateGamePlayerIfAvailable(ctx, gameID, userID, cardSN)
}
