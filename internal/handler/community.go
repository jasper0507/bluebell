package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jasper0507/bluebell/internal/response"
	"github.com/jasper0507/bluebell/internal/service"
)

type CommunityHandler struct {
	communityService *service.CommunityService
}

func NewCommunityHandler(communityService *service.CommunityService) *CommunityHandler {
	return &CommunityHandler{
		communityService: communityService,
	}
}

// communityListItemResponse 社区列表项响应
type communityListItemResponse struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
}

// communityDetailResponse 社区详情响应
type communityDetailResponse struct {
	ID           uint      `json:"id"`
	Name         string    `json:"name"`
	Introduction string    `json:"introduction"`
	CreatedAt    time.Time `json:"created_at"`
}

// List 获取社区列表
func (h *CommunityHandler) List(c *gin.Context) {
	// 1. 获取社区列表
	communities, err := h.communityService.List(c.Request.Context())

	if err != nil {
		slog.Error("获取社区列表失败", "error", err)
		response.Error(c, response.CodeInternalError)
		return
	}

	// 2. 构建响应数据
	data := make([]communityListItemResponse, 0, len(communities))

	for _, community := range communities {
		data = append(data, communityListItemResponse{
			ID:   community.ID,
			Name: community.Name,
		})
	}

	// 3. 返回响应
	response.Success(c, http.StatusOK, data)
}

// Detail 获取社区详情
func (h *CommunityHandler) Detail(c *gin.Context) {
	// 1. 获取社区id
	communityID, ok := parseUintParam(c, "communityID")
	if !ok {
		response.Error(c, response.CodeInvalidParams)
		return
	}

	// 2. 获取社区详情
	community, err := h.communityService.Detail(c.Request.Context(), communityID)

	if errors.Is(err, service.ErrCommunityNotFound) {
		response.Error(c, response.CodeCommunityNotFound)
		return
	}

	if err != nil {
		slog.Error("获取社区详情失败", "community_id", communityID, "error", err)
		response.Error(c, response.CodeInternalError)
		return
	}

	// 3. 构建响应数据
	data := communityDetailResponse{
		ID:           community.ID,
		Name:         community.Name,
		Introduction: community.Introduction,
		CreatedAt:    community.CreatedAt,
	}

	// 4. 返回响应
	response.Success(c, http.StatusOK, data)
}
