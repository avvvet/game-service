package service

import (
	"context"
	"fmt"

	"github.com/avvvet/bingo-services/internal/gamesvc/models"
	"github.com/avvvet/bingo-services/internal/gamesvc/store"
)

type GameService struct {
	gameStore *store.GameStore
}

func NewGameService(gameStore *store.GameStore) *GameService {
	return &GameService{gameStore: gameStore}
}
func (s *GameService) GetGameByID(ctx context.Context, gameID int) (*models.Game, error) {
	return s.gameStore.GetGameByID(ctx, gameID)
}

// GameService methods with fee to ID conversion

// GetGameByFeeAndStatus accepts a game type fee and converts it to ID
func (s *GameService) GetGameByFeeAndStatus(ctx context.Context, gameTypeFee int, status string) (*models.Game, error) {
	// Convert game type fee to ID
	gameTypeID, err := s.convertFeeToGameTypeID(gameTypeFee)
	if err != nil {
		return nil, err
	}

	// Call the existing method with the converted ID
	return s.GetGameByTypeAndStatus(ctx, gameTypeID, status)
}

// convertFeeToGameTypeID converts game type fee to corresponding ID
func (s *GameService) convertFeeToGameTypeID(fee int) (int, error) {
	switch fee {
	case 10:
		return 1, nil
	case 20:
		return 2, nil
	case 40:
		return 3, nil
	case 50:
		return 4, nil
	case 100:
		return 5, nil
	case 200:
		return 6, nil
	default:
		return 0, fmt.Errorf("invalid game type fee: %d", fee)
	}
}

// convertGameTypeIDToFee converts game type ID back to fee (for response mapping)
func (s *GameService) convertGameTypeIDToFee(gameTypeID int) (int, error) {
	switch gameTypeID {
	case 1:
		return 10, nil
	case 2:
		return 20, nil
	case 3:
		return 40, nil
	case 4:
		return 50, nil
	case 5:
		return 100, nil
	case 6:
		return 200, nil
	default:
		return 0, fmt.Errorf("invalid game type ID: %d", gameTypeID)
	}
}

// Original method remains unchanged
func (s *GameService) GetGameByTypeAndStatus(ctx context.Context, gameTypeID int, status string) (*models.Game, error) {
	// Fetch game from the store
	game, err := s.gameStore.GetGameByTypeAndStatus(ctx, gameTypeID, status)
	if err != nil {
		return nil, err
	}
	return game, nil
}
