package service

import (
	"context"
	"errors"

	"github.com/jasper0507/bluebell/internal/model"
	"github.com/jasper0507/bluebell/internal/repository"
)

type CommunityService struct {
	communityRepo *repository.CommunityRepository
}

func NewCommunityService(communityRepo *repository.CommunityRepository) *CommunityService {
	return &CommunityService{
		communityRepo: communityRepo,
	}
}

var ErrCommunityNotFound = errors.New("社区不存在")

// List 获取社区列表
func (s *CommunityService) List(ctx context.Context) ([]model.Community, error) {
	return s.communityRepo.List(ctx)
}

// Detail 获取社区详情
func (s *CommunityService) Detail(ctx context.Context, id uint) (*model.Community, error) {
	return s.communityRepo.FindByID(ctx, id)
}
