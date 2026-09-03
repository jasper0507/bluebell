package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jasper0507/bluebell/internal/model"

	"gorm.io/gorm"
)

type UserRepository struct {
	db *gorm.DB
}

func NewUserRepository(db *gorm.DB) *UserRepository {
	return &UserRepository{db: db}
}

// ExistsByUsername 通过用户名检查用户是否存在
func (r *UserRepository) ExistsByUsername(ctx context.Context, username string) (bool, error) {
	count, err := gorm.G[model.User](r.db).Where("username = ?", username).Count(ctx, "*")

	if err != nil {
		return false, fmt.Errorf("query username: %w", err)
	}

	return count > 0, nil
}

// Create 创建用户
func (r *UserRepository) Create(ctx context.Context, user *model.User) error {
	err := gorm.G[model.User](r.db).Create(ctx, user)

	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return errors.New("username already exists")
	}

	if err != nil {
		return fmt.Errorf("insert user: %w", err)
	}

	return nil
}
