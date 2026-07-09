package config

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

// Config 为微服务运行所需的全部配置，加载优先级（由低到高）：
// 内置默认值 < config.yaml < 环境变量。
type Config struct {
	HTTPAddr     string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration

	V4XdbPath string
	V6XdbPath string

	// CachePolicy: file | vectorindex | content
	// content 为全内存缓存（默认，性能最佳），vectorindex 仅缓存向量索引（~512KB）。
	CachePolicy string
	Searchers   int

	MaxBatchSize int

	// PIDFile 为可选 PID 文件路径；非空时服务启动时写入自身 PID，退出时清理，
	// 便于运维脚本（如 xdb 更新后）向该 PID 发送信号触发热加载。
	PIDFile string
}

// yamlConfig 为 YAML 配置的中间结构，duration 以字符串形式书写，便于人工维护。
type yamlConfig struct {
	HTTPAddr     string `yaml:"http_addr"`
	ReadTimeout  string `yaml:"read_timeout"`
	WriteTimeout string `yaml:"write_timeout"`
	V4XdbPath    string `yaml:"v4_xdb_path"`
	V6XdbPath    string `yaml:"v6_xdb_path"`
	CachePolicy  string `yaml:"cache_policy"`
	Searchers    int    `yaml:"searchers"`
	MaxBatchSize int    `yaml:"max_batch_size"`
	PIDFile      string `yaml:"pid_file"`
}

// Load 按优先级合并默认值、配置文件与环境变量，校验并返回。
func Load() (*Config, error) {
	cfg := &Config{
		HTTPAddr:     ":8080",
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		// xdb 路径由部署方显式提供（挂载/配置），默认留空，避免误用不存在的默认路径
		V4XdbPath:    "",
		V6XdbPath:    "",
		CachePolicy:  "content",
		Searchers:    runtime.GOMAXPROCS(0) * 2,
		MaxBatchSize: 1000,
		PIDFile:      "",
	}

	if err := loadFromFile(cfg); err != nil {
		return nil, err
	}
	loadFromEnv(cfg)

	if cfg.V4XdbPath == "" && cfg.V6XdbPath == "" {
		return nil, fmt.Errorf("至少需要配置 v4_xdb_path 或 v6_xdb_path 之一")
	}
	return cfg, nil
}

// loadFromFile 读取 YAML 配置文件（路径由 IP2REGION_CONFIG 指定，默认 config.yaml），
// 仅对文件中显式设置的非空字段进行覆盖。
func loadFromFile(cfg *Config) error {
	path := getEnv("IP2REGION_CONFIG", "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("读取配置文件 %s 失败: %w", path, err)
	}

	var yc yamlConfig
	if err := yaml.Unmarshal(data, &yc); err != nil {
		return fmt.Errorf("解析配置文件 %s 失败: %w", path, err)
	}

	if yc.HTTPAddr != "" {
		cfg.HTTPAddr = yc.HTTPAddr
	}
	if yc.ReadTimeout != "" {
		d, err := time.ParseDuration(yc.ReadTimeout)
		if err != nil {
			return fmt.Errorf("read_timeout 非法: %q", yc.ReadTimeout)
		}
		cfg.ReadTimeout = d
	}
	if yc.WriteTimeout != "" {
		d, err := time.ParseDuration(yc.WriteTimeout)
		if err != nil {
			return fmt.Errorf("write_timeout 非法: %q", yc.WriteTimeout)
		}
		cfg.WriteTimeout = d
	}
	if yc.V4XdbPath != "" {
		cfg.V4XdbPath = yc.V4XdbPath
	}
	if yc.V6XdbPath != "" {
		cfg.V6XdbPath = yc.V6XdbPath
	}
	if yc.CachePolicy != "" {
		cfg.CachePolicy = yc.CachePolicy
	}
	if yc.Searchers > 0 {
		cfg.Searchers = yc.Searchers
	}
	if yc.MaxBatchSize > 0 {
		cfg.MaxBatchSize = yc.MaxBatchSize
	}
	if yc.PIDFile != "" {
		cfg.PIDFile = yc.PIDFile
	}
	return nil
}

// loadFromEnv 以环境变量覆盖上述结果，缺省项保持原值。
func loadFromEnv(cfg *Config) {
	if v := os.Getenv("IP2REGION_HTTP_ADDR"); v != "" {
		cfg.HTTPAddr = v
	}
	if v := os.Getenv("IP2REGION_READ_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.ReadTimeout = d
		}
	}
	if v := os.Getenv("IP2REGION_WRITE_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.WriteTimeout = d
		}
	}
	if v := os.Getenv("IP2REGION_V4_XDB"); v != "" {
		cfg.V4XdbPath = v
	}
	if v := os.Getenv("IP2REGION_V6_XDB"); v != "" {
		cfg.V6XdbPath = v
	}
	if v := os.Getenv("IP2REGION_CACHE_POLICY"); v != "" {
		cfg.CachePolicy = v
	}
	if v := os.Getenv("IP2REGION_SEARCHERS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Searchers = n
		}
	}
	if v := os.Getenv("IP2REGION_MAX_BATCH"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxBatchSize = n
		}
	}
	if v := os.Getenv("IP2REGION_PID_FILE"); v != "" {
		cfg.PIDFile = v
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
