package handler

import (
	"log/slog"
	"net/http"
	"strconv"

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

type communityListItem struct {
	CommunityID   uint   `json:"community_id"`
	CommunityName string `json:"community_name"`
}

// List 获取社区列表
func (h *CommunityHandler) List(c *gin.Context) {
	// 1. 获取社区列表
	communities, err := h.communityService.List(c.Request.Context())

	if err != nil {
		slog.Error("获取社区列表失败", "err", err)
		response.Error(c, response.CodeInternalError)
		return
	}

	// 2. 构建响应数据
	data := make([]communityListItem, 0, len(communities))

	for _, community := range communities {
		data = append(data, communityListItem{
			CommunityID:   community.ID,
			CommunityName: community.Name,
		})
	}

	// 3. 返回响应
	response.Success(c, http.StatusOK, data)
}

// Detail 获取社区详情
func (h *CommunityHandler) Detail(c *gin.Context) {
	// 1. 获取社区id
	idstr := c.Param("id")

	id, err := strconv.ParseUint(idstr, 10, 64)
	if err != nil || id == 0 {
		response.Error(c, response.CodeInternalError)
		return
	}

	// 2. 获取社区详情
	communities, err := h.communityService.Detail(c.Request.Context(), uint(id))

	if err != nil {
		slog.Error("获取社区详情失败", "err", err)
		response.Error(c, response.CodeCommunityNotFound)
		return
	}

	// 3. 返回响应
	response.Success(c, http.StatusOK, communities)
}
