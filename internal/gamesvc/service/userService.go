package service

import (
	"context"
	"fmt"
	"log"

	"github.com/avvvet/bingo-services/internal/gamesvc/models"
	"github.com/avvvet/bingo-services/internal/gamesvc/store"
)

// UserService struct represents the user service layer
type UserService struct {
	userStore *store.UserStore
}

// NewUserService creates a new UserService instance
func NewUserService(userStore *store.UserStore) *UserService {
	return &UserService{
		userStore: userStore,
	}
}

// GetOrCreateUser checks if a user exists and creates them if not
func (s *UserService) GetOrCreateUser(userInfo models.User) (*models.User, error) {
	// Try to fetch user from the store (by user_id)
	existingUser, err := s.userStore.GetByID(context.Background(), userInfo.UserId)
	if err != nil {
		log.Printf("User not found, creating new user: %v", err)

		// Create the user if not found
		userInfo.Status = "ACTIVE"
		userId, err := s.userStore.CreateUser(context.Background(), userInfo)
		if err != nil {
			return nil, fmt.Errorf("failed to create user: %v", err)
		}

		// Return the created user
		userInfo.UserId = userId
		return &userInfo, nil
	}

	// Return the existing user if found
	return existingUser, nil
}
