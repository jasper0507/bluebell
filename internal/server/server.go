package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/jasper0507/bluebell/internal/config"
)

// Run 启动 HTTP 服务，并在收到退出信号后优雅关闭。
func Run(handler http.Handler, cfg *config.HTTPConfig) error {
	// 1. 创建服务器
	srv := &http.Server{
		Addr:    cfg.Addr,
		Handler: handler,
	}

	serverErr := make(chan error, 1)

	// 2. 并发启动服务器
	go func() {
		serverErr <- srv.ListenAndServe()
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)

	select {
	case err := <-serverErr:
		// 排除服务器被正常关闭的error，返回真错误
		if err != http.ErrServerClosed {
			return fmt.Errorf("listen: %w", err)
		}
		return nil

	case <-quit:
	}

	// 3. 创建一个`shutdownTimeout`秒后自动超时的上下文
	ctx, cancel := context.WithTimeout(
		context.Background(),
		cfg.ShutdownTimeout,
	)
	// 确保Shutdown用完就释放
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}

	return nil
}
