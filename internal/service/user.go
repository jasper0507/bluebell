package service

import (
	"context"
	"errors"
	"uuid"

	"github.com/jasper0507/bluebell/internal/model"
	"github.com/jasper0507/bluebell/internal/repository"
)

type UserService struct {
	userRepo     *repository.UserRepository
	tokenService *TokenService
}

func NewUserService(userRepo *repository.UserRepository, tokenService *TokenService) *UserService {
	return &UserService{
		userRepo:     userRepo,
		tokenService: tokenService,
	}
}

var (
	ErrUsernameExists     = repository.ErrUsernameExists
	ErrInvalidCredentials = errors.New("用户名或密码错误")
)

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
		Name:         username,
		PasswordHash: passwordHash,
	}

	// 4. 保存用户
	if err := s.userRepo.Create(ctx, user); err != nil {
		return err
	}

	return nil
}

// Login 用户登录
func (s *UserService) Login(ctx context.Context, username, password string) (string, error) {
	// 1. 根据用户名查找用户
	user, err := s.userRepo.FindByUsername(ctx, username)

	// 用户不存在
	if errors.Is(err, repository.ErrUserNotFound) {
		return "", ErrInvalidCredentials
	}

	// 数据库查询失败
	if err != nil {
		return "", err
	}

	// 2. 检验密码
	if err := verifyPassword(user.PasswordHash, password); err != nil {
		return "", ErrInvalidCredentials
	}

	// 3. 生成访问令牌
	accessToken, err := s.tokenService.GenerateAccessToken(user.UserID)

	if err != nil {
		return "", err
	}

	return accessToken, nil
}
