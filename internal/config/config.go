package config

import (
	"fmt"
	"os"
	"strings"

	"time"

	"github.com/spf13/viper"
)

const defaultConfigFile = "configs/config.yaml"

// Viper 读取配置文件后，会通过 mapstructure 标签将配置项映射到对应结构体字段。
type Config struct {
	HTTP  HTTPConfig  `mapstructure:"http"`
	MySQL MySQLConfig `mapstructure:"mysql"`
	Redis RedisConfig `mapstructure:"redis"`
	Log   LogConfig   `mapstructure:"log"`
}

type HTTPConfig struct {
	Addr            string        `mapstructure:"addr"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
}

type MySQLConfig struct {
	Host            string        `mapstructure:"host"`
	Port            int           `mapstructure:"port"`
	Username        string        `mapstructure:"username"`
	Password        string        `mapstructure:"password"`
	Database        string        `mapstructure:"database"`
	MaxOpenConns    int           `mapstructure:"max_open_conns"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
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

	// MySQL 默认值
	v.SetDefault("mysql.host", "127.0.0.1")
	v.SetDefault("mysql.port", 3306)
	v.SetDefault("mysql.max_open_conns", 10)
	v.SetDefault("mysql.max_idle_conns", 10)
	v.SetDefault("mysql.conn_max_lifetime", 3*time.Minute)

	// Redis 默认值
	v.SetDefault("redis.addr", "127.0.0.1:6379")
	v.SetDefault("redis.password", "")
	v.SetDefault("redis.db", 0)

	// Log 默认值（避免空字符串导致 logger 初始化报错）
	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "json")
}

func Load() (*Config, error) {
	// 允许部署时通过环境变量指定其他配置文件路径
	// 未设置时使用项目默认配置文件
	configFile := os.Getenv("GOWEB_CONFIG_FILE")
	if configFile == "" {
		configFile = defaultConfigFile
	}

	v := viper.New()

	// 设置默认配置项
	setDefaultConfig(v)

	// 1. 确定配置文件路径
	v.SetConfigFile(configFile)

	// 2. 允许环境变量覆盖配置文件中的值。
	// 环境变量统一使用 GOWEB_ 前缀
	v.SetEnvPrefix("GOWEB")

	// 将配置键中的 "." 替换为 "_"
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// 启用环境变量覆盖
	// 如果配置文件和环境变量同时提供某个配置项
	// Viper 会优先使用对应的环境变量值
	v.AutomaticEnv()

	// 3. 读取 YAML 配置文件
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read config %q: %w", configFile, err)
	}

	//  4. 将最终配置反序列化为 Config 结构体。
	// 从这里开始，项目其他部分只需要使用 Config
	// 不需要直接依赖 Viper
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	return &cfg, nil
}
