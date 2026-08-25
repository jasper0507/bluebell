package router

import (
	"github.com/gin-gonic/gin"
	"github.com/jasper0507/bluebell/internal/handler"
	"github.com/jasper0507/bluebell/internal/middleware"
)

func New() *gin.Engine {
	r := gin.New()
	r.Use(
		middleware.RequestLogger(),
		gin.Recovery(),
	)

	api := r.Group("/api/v1")
	{
		api.GET("/ping", handler.Ping)
		api.GET("/health", handler.Healthz)
	}

	return r
}
