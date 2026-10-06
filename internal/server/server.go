package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/jasper0507/bluebell/internal/config"
)

// Run 启动 HTTP 服务，并在上下文取消后优雅关闭
func Run(
	ctx context.Context,
	handler http.Handler,
	cfg *config.HTTPConfig,
) error {
	// 1. 创建 HTTP 服务器
	srv := &http.Server{
		Addr:    cfg.Addr,
		Handler: handler,
	}

	serverErr := make(chan error, 1)

	// 2. 并发启动 HTTP 服务
	go func() {
		serverErr <- srv.ListenAndServe()
	}()

	// 3. 等待服务器异常退出或应用上下文取消
	select {
	case err := <-serverErr:
		// 排除服务器被正常关闭的错误，只返回真正的运行错误
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}

		return fmt.Errorf("listen: %w", err)

	case <-ctx.Done():
	}

	// 4. 创建 shutdownTimeout 后自动超时的关闭上下文
	shutdownCtx, cancel := context.WithTimeout(
		context.Background(),
		cfg.ShutdownTimeout,
	)
	defer cancel()

	// 5. 等待正在处理的请求结束后关闭 HTTP 服务
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}

	return nil
}
