package migrate

import (
	"fmt"
	"log"

	"github.com/jasper0507/bluebell/internal/config"
	"github.com/jasper0507/bluebell/internal/database"
	"github.com/jasper0507/bluebell/internal/model"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	// 加载配置
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// 初始化数据库连接
	db, err := database.Open(&cfg.MySQL)
	if err != nil {
		return fmt.Errorf("init MySQL: %w", err)
	}

	defer func() {
		if err := database.Close(db); err != nil {
			log.Printf("close MySQL: %v", err)
		}
	}()

	// 执行数据库迁移
	if err := db.AutoMigrate(&model.User{}); err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}

	log.Printf("database migration completed")

	return nil
}
