package config

import (
	"fmt"
	"os"
	"strings"

	"time"

	"github.com/spf13/viper"
)

const defaultConfigFile = "configs/config.yaml"

// Config 定义 HTTP、MySQL、Redis、日志、认证和限流配置。
type Config struct {
	HTTP      HTTPConfig      `mapstructure:"http"`
	MySQL     MySQLConfig     `mapstructure:"mysql"`
	Redis     RedisConfig     `mapstructure:"redis"`
	Log       LogConfig       `mapstructure:"log"`
	Auth      AuthConfig      `mapstructure:"auth"`
	RateLimit RateLimitConfig `mapstructure:"rate_limit"`
}

// RateLimitRule 令牌桶参数：每秒补充 Rate 个令牌，最多积攒 Burst 个
type RateLimitRule struct {
	Rate  float64 `mapstructure:"rate"`
	Burst int     `mapstructure:"burst"`
}

type RateLimitConfig struct {
	Enabled bool          `mapstructure:"enabled"`
	API     RateLimitRule `mapstructure:"api"`
	Auth    RateLimitRule `mapstructure:"auth"`
}

type AuthConfig struct {
	Secret          string        `mapstructure:"secret"`
	Issuer          string        `mapstructure:"issuer"`
	AccessTokenTTL  time.Duration `mapstructure:"access_token_ttl"`
	RefreshTokenTTL time.Duration `mapstructure:"refresh_token_ttl"`
	CookieSecure    bool          `mapstructure:"cookie_secure"`
}

type HTTPConfig struct {
	Addr            string        `mapstructure:"addr"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
	TrustedProxies  []string      `mapstructure:"trusted_proxies"`
	ReadTimeout     time.Duration `mapstructure:"read_timeout"`
	WriteTimeout    time.Duration `mapstructure:"write_timeout"`
	MaxBodyBytes    int64         `mapstructure:"max_body_bytes"`
}

type MySQLConfig struct {
	Host              string        `mapstructure:"host"`
	Port              int           `mapstructure:"port"`
	Username          string        `mapstructure:"username"`
	Password          string        `mapstructure:"password"`
	Database          string        `mapstructure:"database"`
	MaxOpenConns      int           `mapstructure:"max_open_conns"`
	MaxIdleConns      int           `mapstructure:"max_idle_conns"`
	ConnMaxLifetime   time.Duration `mapstructure:"conn_max_lifetime"`
	InterpolateParams bool          `mapstructure:"interpolate_params"`
}

type RedisConfig struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

type LogConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

func setDefaultConfig(v *viper.Viper) {
	// HTTP 默认值
	v.SetDefault("http.addr", ":8080")
	v.SetDefault("http.shutdown_timeout", 5*time.Second)
	v.SetDefault("http.trusted_proxies", []string{})
	v.SetDefault("http.read_timeout", 10*time.Second)
	v.SetDefault("http.write_timeout", 10*time.Second)
	v.SetDefault("http.max_body_bytes", 1<<20)

	// MySQL 默认值
	v.SetDefault("mysql.host", "127.0.0.1")
	v.SetDefault("mysql.port", 3306)
	v.SetDefault("mysql.max_open_conns", 10)
	v.SetDefault("mysql.max_idle_conns", 10)
	v.SetDefault("mysql.conn_max_lifetime", 3*time.Minute)
	v.SetDefault("mysql.interpolate_params", true)

	// Redis 默认值
	v.SetDefault("redis.addr", "127.0.0.1:6379")
	v.SetDefault("redis.password", "")
	v.SetDefault("redis.db", 0)

	// Log 默认值（避免空字符串导致 logger 初始化报错）
	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "json")

	// Auth 默认值
	v.SetDefault("auth.secret", "")
	v.SetDefault("auth.issuer", "ripple")
	v.SetDefault("auth.access_token_ttl", 15*time.Minute)
	v.SetDefault("auth.refresh_token_ttl", 7*24*time.Hour)
	v.SetDefault("auth.cookie_secure", false)

	// RateLimit 默认值
	v.SetDefault("rate_limit.enabled", true)
	v.SetDefault("rate_limit.api.rate", 20)
	v.SetDefault("rate_limit.api.burst", 40)
	v.SetDefault("rate_limit.auth.rate", 0.2)
	v.SetDefault("rate_limit.auth.burst", 5)
}

func Load() (*Config, error) {
	// 允许部署时通过环境变量指定其他配置文件路径
	// 未设置时使用项目默认配置文件
	configFile := os.Getenv("RIPPLE_CONFIG_FILE")
	if configFile == "" {
		configFile = defaultConfigFile
	}

	v := viper.New()

	// 设置默认配置项
	setDefaultConfig(v)

	// 1. 确定配置文件路径
	v.SetConfigFile(configFile)

	// 2. 允许环境变量覆盖配置文件中的值。
	// 环境变量统一使用前缀
	v.SetEnvPrefix("RIPPLE")

	// 将配置键中的 "." 替换为 "_"
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// 启用环境变量覆盖
	// 如果配置文件和环境变量同时提供某个配置项
	// Viper 会优先使用对应的环境变量值
	v.AutomaticEnv()

	// 3. 读取 YAML 配置文件
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("读取配置文件 %q 失败: %w", configFile, err)
	}

	// 4. 将最终配置反序列化为 Config 结构体。
	// Viper 通过 mapstructure 标签将配置项映射到对应字段。
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}

	return &cfg, nil
}
