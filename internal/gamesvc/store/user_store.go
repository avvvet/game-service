package store

import (
	"context"
	"fmt"

	"github.com/avvvet/bingo-services/internal/gamesvc/models"

	"github.com/jackc/pgx/v5/pgxpool"
)

type UserStore struct {
	db *pgxpool.Pool
}

func NewUserStore(db *pgxpool.Pool) *UserStore {
	return &UserStore{db: db}
}

func (r *UserStore) CreateUser(ctx context.Context, user models.User) (int64, error) {
	var userId int64

	// Use INSERT to create a new user
	query := `
        INSERT INTO users (user_id, name, email, phone, avatar, status)
        VALUES ($1, $2, $3, $4, $5, $6)
        RETURNING user_id;
    `

	// Execute the query and get the user_id
	err := r.db.QueryRow(ctx, query, user.UserId, user.Name, user.Email, user.Phone, user.Avatar, user.Status).Scan(&userId)
	if err != nil {
		return 0, fmt.Errorf("could not create user: %v", err)
	}

	return userId, nil
}

func (r *UserStore) GetByIDOld(ctx context.Context, id int64) (*models.User, error) {
	row := r.db.QueryRow(ctx, "SELECT user_id, name, email, phone, avatar, status, created_at, updated_at FROM users WHERE user_id=$1", id)
	u := &models.User{}
	err := row.Scan(row)
	return u, err
}

func (r *UserStore) GetByID(ctx context.Context, id int64) (*models.User, error) {
	row := r.db.QueryRow(ctx, `
        SELECT user_id, name, email, phone, avatar, status, created_at, updated_at 
        FROM users 
        WHERE user_id = $1
    `, id)

	u := &models.User{}
	err := row.Scan(
		&u.UserId,
		&u.Name,
		&u.Email,
		&u.Phone,
		&u.Avatar,
		&u.Status,
		&u.CreatedAt,
		&u.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	return u, nil
}
