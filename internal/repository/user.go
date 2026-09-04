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

var (
	ErrUserNotFound   = errors.New("用户不存在")
	ErrUsernameExists = errors.New("用户名已存在")
)

// ExistsByUsername 通过用户名检查用户是否存在
func (r *UserRepository) ExistsByUsername(ctx context.Context, username string) error {
	count, err := gorm.G[model.User](r.db).
		Where("username = ?", username).
		Count(ctx, "*")

	// 数据库查询失败
	if err != nil {
		return fmt.Errorf("查询用户名失败: %w", err)
	}

	// 用户名已存在
	if count > 0 {
		return ErrUsernameExists
	}

	return nil
}

// Create 创建用户
func (r *UserRepository) Create(ctx context.Context, user *model.User) error {
	err := gorm.G[model.User](r.db).Create(ctx, user)

	// 用户名已存在
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return ErrUsernameExists
	}

	// 数据库插入失败
	if err != nil {
		return fmt.Errorf("插入用户失败: %w", err)
	}

	return nil
}

// FindByUsername 通过用户名查找用户
func (r *UserRepository) FindByUsername(ctx context.Context, username string) (*model.User, error) {
	user, err := gorm.G[model.User](r.db).
		Where("username = ?", username).
		First(ctx)

	// 用户不存在
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrUserNotFound
	}

	// 数据库查询失败
	if err != nil {
		return nil, fmt.Errorf("根据用户名查询用户失败: %w", err)
	}

	return &user, nil
}
