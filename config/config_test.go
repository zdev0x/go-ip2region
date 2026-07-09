package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoad_Precedence(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "config.yaml")
	yamlContent := `
http_addr: ":9090"
read_timeout: "3s"
cache_policy: "vectorindex"
searchers: 7
v4_xdb_path: "/tmp/xdb_v4.xdb"
`
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("IP2REGION_CONFIG", yamlPath)
	// 环境变量覆盖 cache_policy 与 v4 路径
	t.Setenv("IP2REGION_CACHE_POLICY", "content")
	t.Setenv("IP2REGION_V6_XDB", "/tmp/xdb_v6.xdb")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}

	if cfg.HTTPAddr != ":9090" {
		t.Fatalf("http_addr 应来自 yaml，got %q", cfg.HTTPAddr)
	}
	if cfg.ReadTimeout != 3*time.Second {
		t.Fatalf("read_timeout 应来自 yaml，got %v", cfg.ReadTimeout)
	}
	if cfg.Searchers != 7 {
		t.Fatalf("searchers 应来自 yaml，got %d", cfg.Searchers)
	}
	// 环境变量覆盖 yaml
	if cfg.CachePolicy != "content" {
		t.Fatalf("cache_policy 应被环境变量覆盖，got %q", cfg.CachePolicy)
	}
	// 环境变量补充 yaml 未设置的字段
	if cfg.V6XdbPath != "/tmp/xdb_v6.xdb" {
		t.Fatalf("v6_xdb_path 应来自环境变量，got %q", cfg.V6XdbPath)
	}
}

func TestLoad_RequiresXdb(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "config.yaml")
	// 两个路径均为空 -> 校验失败
	yamlContent := `v4_xdb_path: ""` + "\n" + `v6_xdb_path: ""`
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("IP2REGION_CONFIG", yamlPath)
	if _, err := Load(); err == nil {
		t.Fatal("两个 xdb 路径均为空时应返回错误")
	}
}
