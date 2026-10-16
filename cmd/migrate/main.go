package main

import (
	"fmt"
	"log"

	"github.com/jasper0507/ripple/internal/config"
	"github.com/jasper0507/ripple/internal/database"
	"github.com/jasper0507/ripple/internal/model"
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
		return fmt.Errorf("加载配置失败: %w", err)
	}

	// 初始化数据库连接
	db, err := database.Open(&cfg.MySQL)
	if err != nil {
		return fmt.Errorf("初始化 MySQL 失败: %w", err)
	}

	defer func() {
		if err := database.Close(db); err != nil {
			log.Printf("关闭 MySQL 失败: %v", err)
		}
	}()

	// 执行数据库迁移
	if err := db.AutoMigrate(model.All()...); err != nil {
		return fmt.Errorf("数据库迁移失败: %w", err)
	}

	log.Printf("数据库迁移完成")

	return nil
}
