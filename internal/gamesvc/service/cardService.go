package service

import (
	"context"

	"github.com/avvvet/bingo-services/internal/gamesvc/models"
	"github.com/avvvet/bingo-services/internal/gamesvc/store"
)

type CardService struct {
	store *store.CardStore
}

func NewCardService(store *store.CardStore) *CardService {
	return &CardService{store: store}
}

func (s *CardService) GetCardBySN(ctx context.Context, sn string) (*models.Card, error) {
	return s.store.GetCardBySN(ctx, sn)
}
