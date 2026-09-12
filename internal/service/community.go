package service

import (
	"context"

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

func (s *CommunityService) List(ctx context.Context) ([]model.Community, error) {
	return s.communityRepo.List(ctx)
}
