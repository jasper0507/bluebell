package logger

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/jasper0507/bluebell/internal/config"
)

func New(cfg *config.LogConfig) (*slog.Logger, error) {
	// 将配置文件中的 string 类型转换为 slog.Level。
	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.Level)); err != nil {
		return nil, fmt.Errorf("parse log level %q: %w", cfg.Level, err)
	}

	// Level 表示最低记录级别，低于该级别的日志会被过滤。
	options := &slog.HandlerOptions{
		Level: level,
	}

	// text 更适合本地开发时直接在终端阅读；
	// json 更适合生产环境交给日志采集系统解析。
	var handler slog.Handler
	switch strings.ToLower(cfg.Format) {
	case "json":
		handler = slog.NewJSONHandler(os.Stdout, options)
	case "text":
		handler = slog.NewTextHandler(os.Stdout, options)
	default:
		return nil, fmt.Errorf("unsupported log format %q", cfg.Format)
	}

	return slog.New(handler), nil
}
