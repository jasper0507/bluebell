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
func (r *UserRepository) ExistsByUsername(ctx context.Context, username string) error {
	count, err := gorm.G[model.User](r.db).Where("username = ?", username).Count(ctx, "*")

	// 数据库查询失败
	if err != nil {
		return fmt.Errorf("query username: %w", err)
	}

	// 用户名已存在
	if count > 0 {
		return errors.New("username already exists")
	}

	return nil
}

// Create 创建用户
func (r *UserRepository) Create(ctx context.Context, user *model.User) error {
	err := gorm.G[model.User](r.db).Create(ctx, user)

	// 用户名已存在
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return errors.New("username already exists")
	}

	// 数据库插入失败
	if err != nil {
		return fmt.Errorf("insert user: %w", err)
	}

	return nil
}
