package service

import (
	"context"
	"errors"
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

// UserRegister 注册用户
func (s *UserService) UserRegister(ctx context.Context, username, password string) error {
	// 判断用户是否存在
	exists, err := s.userRepo.ExistsByUsername(ctx, username)

	if err != nil {
		return err
	}

	if exists {
		return errors.New("username already exists")
	}

	// 哈希密码
	passwordHash, err := hashPassword(password)
	if err != nil {
		return err
	}

	// 构建User
	user := &model.User{
		UserID:       uuid.NewV7().String(),
		Username:     username,
		PasswordHash: passwordHash,
	}

	// 保存进数据库
	if err := s.userRepo.Create(ctx, user); err != nil {
		return err
	}

	return nil
}
