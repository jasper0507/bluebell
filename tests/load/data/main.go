// 固定压测数据的初始化与 Redis 重建；施压、鉴权和报告由原生 k6 完成。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
	"uuid"

	"github.com/jasper0507/bluebell/internal/cache"
	"github.com/jasper0507/bluebell/internal/config"
	"github.com/jasper0507/bluebell/internal/database"
	"github.com/jasper0507/bluebell/internal/model"
	"github.com/jasper0507/bluebell/internal/store"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const password = "Loadtest123456"

func main() {
	mode := flag.String("mode", "prepare", "seed / prepare")
	output := flag.String("output", "tests/load/output/state/data.json", "k6 数据文件")
	flag.Parse()
	if err := run(*mode, *output); err != nil {
		log.Fatal(err)
	}
}

func run(mode, output string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	// 校验环境变量覆盖后的配置，恢复操作仅允许独立压测库。
	if cfg.MySQL.Database != "bluebell_k6_verify" || cfg.MySQL.Username != "loadtest" ||
		cfg.MySQL.Host != "127.0.0.1" || cfg.MySQL.Port != 3306 ||
		cfg.Redis.Addr != "127.0.0.1:6379" || cfg.Redis.DB != 13 || cfg.HTTP.Addr != "127.0.0.1:18080" {
		return fmt.Errorf("只允许 loadtest 账号、本机 bluebell_k6_verify、Redis DB13 和端口18080")
	}
	if mode != "seed" && mode != "prepare" {
		return fmt.Errorf("未知 mode: %s", mode)
	}
	db, err := database.Open(&cfg.MySQL)
	if err != nil {
		return err
	}
	defer database.Close(db)
	if mode == "seed" {
		return seed(db)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	rdb, err := cache.Open(ctx, &cfg.Redis)
	if err != nil {
		return err
	}
	defer rdb.Close()
	return prepare(ctx, db, rdb, cfg, output)
}

func seed(db *gorm.DB) error {
	if err := db.AutoMigrate(&model.User{}, &model.Community{}, &model.Post{}, &model.Comment{}, &model.PostVote{}, &model.OutboxEvent{}); err != nil {
		return err
	}
	var users []model.User
	var communities []model.Community
	var posts []model.Post
	if err := db.Order("id").Find(&users).Error; err != nil {
		return err
	}
	if err := db.Order("id").Find(&communities).Error; err != nil {
		return err
	}
	if err := db.Order("id").Find(&posts).Error; err != nil {
		return err
	}
	empty := len(users) == 0 && len(communities) == 0 && len(posts) == 0
	if !empty && (len(users) != 1 || len(communities) != 1 || len(posts) != 1000) {
		return fmt.Errorf("seed 只接受空库或清理后的 1用户/1社区/1000帖子；重复运行请使用 reset")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if empty {
			u := model.User{UserID: uuid.NewV7().String(), Username: "lt000", PasswordHash: string(hash)}
			c := model.Community{Name: "K6Verification", Introduction: "本机压测社区"}
			if err := tx.Create(&u).Error; err != nil {
				return err
			}
			if err := tx.Create(&c).Error; err != nil {
				return err
			}
			users, communities = []model.User{u}, []model.Community{c}
			posts = make([]model.Post, 1000)
			for i := range posts {
				posts[i] = model.Post{Title: fmt.Sprintf("loadtest seed original %d", i), Content: strings.Repeat("x", 4096), AuthorID: u.UserID, CommunityID: c.ID}
			}
			if err := tx.CreateInBatches(&posts, 200).Error; err != nil {
				return err
			}
			comments := make([]model.Comment, 0, 20000)
			for _, p := range posts {
				for j := 0; j < 20; j++ {
					comments = append(comments, model.Comment{PostID: p.ID, AuthorID: u.UserID, Content: strings.Repeat("loadtest comment ", 16)})
				}
			}
			if err := tx.CreateInBatches(&comments, 500).Error; err != nil {
				return err
			}
		}
		users[0].Username, users[0].PasswordHash = "lt000", string(hash)
		if err := tx.Save(&users[0]).Error; err != nil {
			return err
		}
		for i := 1; i < 200; i++ {
			users = append(users, model.User{UserID: uuid.NewV7().String(), Username: fmt.Sprintf("lt%03d", i), PasswordHash: string(hash)})
		}
		if err := tx.CreateInBatches(users[1:], 200).Error; err != nil {
			return err
		}
		for i := 1; i < 5; i++ {
			c := model.Community{Name: fmt.Sprintf("K6Community%d", i+1), Introduction: "本机压测社区"}
			if err := tx.Create(&c).Error; err != nil {
				return err
			}
			communities = append(communities, c)
		}
		// 保留已有的正文、创建时间、评论及其 ID，只调整社区分布。
		for i, p := range posts {
			if err := tx.Model(&model.Post{}).Where("id = ?", p.ID).Update("community_id", communities[i%5].ID).Error; err != nil {
				return err
			}
		}
		added := make([]model.Post, 1000)
		for i := range added {
			added[i] = model.Post{Title: fmt.Sprintf("loadtest seed %d", i), Content: strings.Repeat("x", 4096), AuthorID: users[i%200].UserID, CommunityID: communities[i%5].ID}
		}
		if err := tx.CreateInBatches(&added, 200).Error; err != nil {
			return err
		}
		posts = append(posts, added...)
		votes := make([]model.PostVote, 0, 20000)
		for i, p := range posts {
			for j := 0; j < 10; j++ {
				direction := int8(1)
				if j >= 7 {
					direction = -1
				}
				votes = append(votes, model.PostVote{PostID: p.ID, UserID: users[(i*10+j)%200].UserID, Direction: direction})
			}
		}
		return tx.CreateInBatches(&votes, 500).Error
	})
}

func prepare(ctx context.Context, db *gorm.DB, rdb *redis.Client, cfg *config.Config, output string) error {
	var users []model.User
	var posts []model.Post
	var communities []model.Community
	var votes []model.PostVote
	if err := db.Order("id").Find(&users).Error; err != nil {
		return err
	}
	if err := db.Order("id").Find(&posts).Error; err != nil {
		return err
	}
	if err := db.Order("id").Find(&communities).Error; err != nil {
		return err
	}
	if err := db.Order("post_id, user_id").Find(&votes).Error; err != nil {
		return err
	}
	var comments int64
	if err := db.Model(&model.Comment{}).Count(&comments).Error; err != nil {
		return err
	}
	if len(users) != 200 || len(posts) != 2000 || len(communities) != 5 || comments != 20000 || len(votes) != 20000 {
		return fmt.Errorf("请恢复 baseline.sql：需要 200用户/5社区/2000帖子/20000评论/20000投票")
	}
	if err := rdb.FlushDB(ctx).Err(); err != nil {
		return err
	}
	postStore := store.NewPostStore(rdb)
	byID := make(map[uint]int, len(posts))
	syncs := make([]store.PostSync, len(posts))
	postIDs := make([]uint, 0, len(posts))
	usernames := make([]string, 0, len(users))
	communityIDs := make([]uint, 0, len(communities))
	for _, u := range users {
		usernames = append(usernames, u.Username)
	}
	for _, c := range communities {
		communityIDs = append(communityIDs, c.ID)
	}
	for i, p := range posts {
		byID[p.ID] = i
		postIDs = append(postIDs, p.ID)
		syncs[i] = store.PostSync{
			PostID: p.ID, CommunityID: p.CommunityID, CreatedAt: p.CreatedAt,
			NeedIndexSync: true, Votes: make(map[string]int8),
		}
	}
	for _, v := range votes {
		i, ok := byID[v.PostID]
		if !ok {
			return fmt.Errorf("压测投票引用不存在的帖子 %d", v.PostID)
		}
		syncs[i].Votes[v.UserID] = v.Direction
	}
	// 与 Worker 一样按 256 个帖子分批，每帖同时初始化索引和同步全部投票。
	for start := 0; start < len(syncs); start += 256 {
		batch := syncs[start:min(start+256, len(syncs))]
		failed := postStore.SyncPosts(ctx, batch)
		for _, post := range batch {
			if err := failed[post.PostID]; err != nil {
				return fmt.Errorf("重建压测帖子 %d 投影失败: %w", post.PostID, err)
			}
		}
	}
	// 只记录非敏感的实际配置，避免优化前后暗中改变连接池或日志。
	data := map[string]any{
		"users": usernames, "posts": postIDs, "communities": communityIDs, "password": password,
		"dataset": map[string]any{"users": len(users), "posts": len(posts), "communities": len(communities), "comments": comments, "votes": len(votes)},
		"config": map[string]any{
			"mysql_max_open_conns": cfg.MySQL.MaxOpenConns, "mysql_max_idle_conns": cfg.MySQL.MaxIdleConns,
			"mysql_conn_max_lifetime": cfg.MySQL.ConnMaxLifetime.String(), "log_level": cfg.Log.Level, "log_format": cfg.Log.Format,
			"access_token_ttl_seconds": cfg.Auth.AccessTokenTTL.Seconds(), "refresh_token_ttl_seconds": cfg.Auth.RefreshTokenTTL.Seconds(),
		},
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if err := os.WriteFile(output, encoded, 0600); err != nil {
		return err
	}
	fmt.Println("ready: 200 users / 5 communities / 2000 posts / 20000 comments / 20000 votes")
	return nil
}
