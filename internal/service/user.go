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

var ErrInvalidCredentials = errors.New("用户名或密码错误")

// UserRegister 注册用户
func (s *UserService) Register(ctx context.Context, username, password string) error {
	// 判断用户是否存在
	if err := s.userRepo.ExistsByUsername(ctx, username); err != nil {
		return err
	}

	// 生成哈希密码
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

func (s *UserService) Login(ctx context.Context, username, password string) (string, error) {
	// 1. 根据用户名查用户，获取密码
	user, err := s.userRepo.FindByUsername(ctx, username)

	if errors.Is(err, repository.ErrUserNotFound) {
		return "", ErrInvalidCredentials
	}

	if err != nil {
		return "", err
	}

	// 2. 检验密码
	if err = verifyPassword(user.PasswordHash, password); err != nil {
		return "", ErrInvalidCredentials
	}
	// 3. 登录成功
	return user.UserID, nil
}
