// 本机压测的数据准备和 Outbox 观察工具；不修改业务代码。
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
	"uuid"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jasper0507/bluebell/internal/cache"
	"github.com/jasper0507/bluebell/internal/config"
	"github.com/jasper0507/bluebell/internal/database"
	"github.com/jasper0507/bluebell/internal/model"
	"github.com/jasper0507/bluebell/internal/store"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const password = "Loadtest123456"

type fixtureUser struct {
	Username string `json:"username"`
	Token    string `json:"token"`
}
type fixturePost struct {
	ID      uint `json:"id"`
	Comment uint `json:"comment"`
}
type target struct {
	ID   uint `json:"id"`
	User int  `json:"user"`
}
type metadata struct {
	Endpoint     string `json:"endpoint"`
	Pool         int    `json:"pool"`
	Run          string `json:"run"`
	Password     string `json:"password"`
	Communities  []uint `json:"communities"`
	ExpiresAt    int64  `json:"expires_at"`
	MaxOpenConns int    `json:"max_open_conns"`
}
type fixture struct {
	Meta     []metadata    `json:"meta"`
	Users    []fixtureUser `json:"users"`
	Posts    []fixturePost `json:"posts"`
	Targets  []target      `json:"targets"`
	Sessions []string      `json:"sessions"`
}

func main() {
	mode := flag.String("mode", "prepare", "seed / prepare / observe")
	endpoint := flag.String("endpoint", "posts_time", "被测接口")
	pool := flag.Int("pool", 1000, "一次性资源数量")
	vus := flag.Int("vus", 50, "预分配 VU 数量")
	output := flag.String("output", "tests/load/output/state/data.json", "k6 数据文件")
	flag.Parse()
	if err := run(*mode, *endpoint, *pool, *vus, *output); err != nil {
		log.Fatal(err)
	}
}

func run(mode, endpoint string, pool, vus int, output string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	// 环境变量可能覆盖 YAML，校验最终配置，拒绝业务库和集成测试库。
	if cfg.MySQL.Database != "bluebell_k6_verify" || cfg.MySQL.Username != "loadtest" ||
		cfg.MySQL.Host != "127.0.0.1" || cfg.MySQL.Port != 3306 ||
		cfg.Redis.Addr != "127.0.0.1:6379" || cfg.Redis.DB != 13 || cfg.HTTP.Addr != "127.0.0.1:18080" {
		return fmt.Errorf("只允许 loadtest 账号、本机 bluebell_k6_verify、Redis DB13 和端口18080")
	}
	if pool < 1 || vus < 1 {
		return fmt.Errorf("pool 和 vus 必须为正整数")
	}
	db, err := database.Open(&cfg.MySQL)
	if err != nil {
		return err
	}
	defer database.Close(db)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if mode == "observe" {
		// CSV 只输出采样；SQL 慢日志不能混入数据，查询错误由调用方记录。
		db.Logger = db.Logger.LogMode(gormlogger.Silent)
		return observe(ctx, db)
	}
	rdb, err := cache.Open(ctx, &cfg.Redis)
	if err != nil {
		return err
	}
	defer rdb.Close()
	switch mode {
	case "seed":
		return seed(db)
	case "prepare":
		return prepare(ctx, db, rdb, cfg, endpoint, pool, vus, output)
	default:
		return fmt.Errorf("未知 mode: %s", mode)
	}
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

func prepare(ctx context.Context, db *gorm.DB, rdb *redis.Client, cfg *config.Config, endpoint string, pool, vus int, output string) error {
	var users []model.User
	var posts []model.Post
	var communities []model.Community
	var comments []model.Comment
	if err := db.Order("id").Find(&users).Error; err != nil {
		return err
	}
	if err := db.Order("id").Find(&posts).Error; err != nil {
		return err
	}
	if err := db.Order("id").Find(&communities).Error; err != nil {
		return err
	}
	if err := db.Select("id", "post_id", "author_id").Order("id").Find(&comments).Error; err != nil {
		return err
	}
	if len(users) != 200 || len(posts) != 2000 || len(communities) != 5 || len(comments) != 20000 {
		return fmt.Errorf("请先恢复 baseline.sql；基线应为 200用户/5社区/2000帖子/20000评论")
	}
	data := fixture{Users: []fixtureUser{}, Posts: []fixturePost{}, Targets: []target{}, Sessions: []string{}}
	m := metadata{Endpoint: endpoint, Pool: pool, Run: strconv.FormatInt(time.Now().UnixNano(), 36), Password: password}
	for _, c := range communities {
		m.Communities = append(m.Communities, c.ID)
	}
	data.Meta = []metadata{m}
	now := time.Now()
	data.Meta[0].ExpiresAt = now.Add(cfg.Auth.AccessTokenTTL).Unix()
	data.Meta[0].MaxOpenConns = cfg.MySQL.MaxOpenConns
	for _, u := range users {
		claims := jwt.RegisteredClaims{Issuer: cfg.Auth.Issuer, Subject: u.UserID, IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(cfg.Auth.AccessTokenTTL))}
		token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(cfg.Auth.Secret))
		if err != nil {
			return err
		}
		data.Users = append(data.Users, fixtureUser{Username: u.Username, Token: token})
	}
	firstComment := make(map[uint]uint)
	for _, c := range comments {
		if firstComment[c.PostID] == 0 {
			firstComment[c.PostID] = c.ID
		}
	}
	for i, p := range posts {
		if i < 1000 && firstComment[p.ID] == 0 {
			return fmt.Errorf("种子帖 %d 没有评论", p.ID)
		}
		data.Posts = append(data.Posts, fixturePost{ID: p.ID, Comment: firstComment[p.ID]})
	}
	// 单次资源预先准备，DELETE/退出登录不混入额外 HTTP 请求。
	if endpoint == "delete_post" {
		added := make([]model.Post, pool)
		for i := range added {
			added[i] = model.Post{Title: "loadtest disposable", Content: strings.Repeat("x", 4096), AuthorID: users[i%200].UserID, CommunityID: communities[i%5].ID}
		}
		if err := db.CreateInBatches(&added, 200).Error; err != nil {
			return err
		}
		for i, p := range added {
			data.Targets = append(data.Targets, target{ID: p.ID, User: i % 200})
		}
		posts = append(posts, added...)
	}
	if endpoint == "delete_comment" {
		added := make([]model.Comment, pool)
		for i := range added {
			added[i] = model.Comment{PostID: posts[i%1000].ID, AuthorID: users[i%200].UserID, Content: "loadtest disposable"}
		}
		if err := db.CreateInBatches(&added, 200).Error; err != nil {
			return err
		}
		for i, c := range added {
			data.Targets = append(data.Targets, target{ID: c.ID, User: i % 200})
		}
	}
	if err := rdb.FlushDB(ctx).Err(); err != nil {
		return err
	}
	postStore := store.NewPostStore(rdb)
	byID := make(map[uint]model.Post)
	for _, p := range posts {
		byID[p.ID] = p
		if err := postStore.InitPost(ctx, p.ID, p.CommunityID, p.CreatedAt); err != nil {
			return err
		}
	}
	var votes []model.PostVote
	if err := db.Find(&votes).Error; err != nil {
		return err
	}
	for _, v := range votes {
		p := byID[v.PostID]
		if err := postStore.ApplyVote(ctx, p.ID, p.CommunityID, v.UserID, v.Direction, p.CreatedAt); err != nil {
			return err
		}
	}
	if endpoint == "refresh" || endpoint == "logout" {
		n := vus
		if endpoint == "logout" {
			n = pool
		}
		pipe := rdb.Pipeline()
		for i := 0; i < n; i++ {
			raw := make([]byte, 32)
			if _, err := rand.Read(raw); err != nil {
				return err
			}
			token := base64.RawURLEncoding.EncodeToString(raw)
			hash := sha256.Sum256([]byte(token))
			pipe.Set(ctx, "bluebell:auth:refresh:"+hex.EncodeToString(hash[:]), users[i%200].UserID, cfg.Auth.RefreshTokenTTL)
			data.Sessions = append(data.Sessions, token)
		}
		if _, err := pipe.Exec(ctx); err != nil {
			return err
		}
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if err := os.WriteFile(output, encoded, 0600); err != nil {
		return err
	}
	fmt.Printf("prepared endpoint=%s users=200 posts=2000 comments=20000 votes=20000 pool=%d vus=%d\n", endpoint, pool, vus)
	return nil
}

func observe(ctx context.Context, db *gorm.DB) error {
	fmt.Println("time,pending,oldest_seconds,retries,threads_running,row_lock_waits,row_lock_time_ms")
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		var outbox struct {
			Pending int64
			Oldest  float64
			Retries int64
		}
		if err := db.Raw("SELECT COUNT(*) AS pending, COALESCE(MAX(TIMESTAMPDIFF(MICROSECOND,created_at,NOW(3)))/1000000,0) AS oldest, COALESCE(SUM(retry_count),0) AS retries FROM outbox_events").Scan(&outbox).Error; err != nil {
			return err
		}
		var statuses []struct {
			VariableName string
			Value        string
		}
		if err := db.Raw("SHOW GLOBAL STATUS WHERE Variable_name IN ('Threads_running','Innodb_row_lock_waits','Innodb_row_lock_time')").Scan(&statuses).Error; err != nil {
			return err
		}
		values := make(map[string]string)
		for _, s := range statuses {
			values[s.VariableName] = s.Value
		}
		fmt.Printf("%s,%d,%.3f,%d,%s,%s,%s\n", time.Now().UTC().Format(time.RFC3339Nano), outbox.Pending, outbox.Oldest, outbox.Retries, values["Threads_running"], values["Innodb_row_lock_waits"], values["Innodb_row_lock_time"])
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
