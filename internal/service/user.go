package service

import (
	"context"
	"uuid"

	"github.com/jasper0507/bluebell/internal/model"
	"github.com/jasper0507/bluebell/internal/repository"
)

type UserService struct {
	userRepo *repository.UserRepository
}

func NewUserService(userRepo *repository.UserRepository) *UserService {
	return &UserService{
		userRepo: userRepo,
	}
}

var ErrUsernameExists = repository.ErrUsernameExists

// Register 注册用户
func (s *UserService) Register(ctx context.Context, username, password string) error {
	// 1. 判断用户名是否存在
	if err := s.userRepo.ExistsByUsername(ctx, username); err != nil {
		return err
	}

	// 2. 生成哈希密码
	passwordHash, err := hashPassword(password)
	if err != nil {
		return err
	}

	// 3. 构建用户
	user := &model.User{
		UserID:       uuid.NewV7().String(),
		Username:     username,
		PasswordHash: passwordHash,
	}

	// 4. 保存用户
	if err := s.userRepo.Create(ctx, user); err != nil {
		return err
	}

	return nil
}
