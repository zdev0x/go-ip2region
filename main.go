package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/zdev0x/go-ip2region/config"
	"github.com/zdev0x/go-ip2region/handler"
	"github.com/zdev0x/go-ip2region/ip2region"
	"github.com/zdev0x/go-ip2region/model"
)

// version 由构建时的 -ldflags "-X main.version=..." 注入，可通过 -version 查看。
var version = "dev"

// app 持有当前生效的查询服务，并通过读写锁支持 SIGHUP 热加载（原子替换底层服务，零停机）。
type app struct {
	mu  sync.RWMutex
	svc *ip2region.Service
	cfg *config.Config
}

func newApp(cfg *config.Config) (*app, error) {
	svc, err := ip2region.New(cfg)
	if err != nil {
		return nil, err
	}
	return &app{svc: svc, cfg: cfg}, nil
}

func (a *app) Search(ctx context.Context, ip string) (*model.Region, error) {
	a.mu.RLock()
	svc := a.svc
	a.mu.RUnlock()
	return svc.Search(ctx, ip)
}

func (a *app) BatchSearch(ctx context.Context, ips []string) []model.BatchItem {
	a.mu.RLock()
	svc := a.svc
	a.mu.RUnlock()
	return svc.BatchSearch(ctx, ips)
}

// Reload 重新加载 xdb；失败时保留旧服务，不影响线上查询。
func (a *app) Reload() error {
	newSvc, err := ip2region.New(a.cfg)
	if err != nil {
		return err
	}
	a.mu.Lock()
	old := a.svc
	a.svc = newSvc
	a.mu.Unlock()
	if old != nil {
		old.Close()
	}
	return nil
}

func (a *app) Close() error {
	a.mu.RLock()
	svc := a.svc
	a.mu.RUnlock()
	if svc != nil {
		return svc.Close()
	}
	return nil
}

func main() {
	configPath := flag.String("config", "", "配置文件路径（默认 config.yaml，亦可用环境变量 IP2REGION_CONFIG 指定）")
	showVersion := flag.Bool("version", false, "打印版本号并退出")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	if *configPath != "" {
		_ = os.Setenv("IP2REGION_CONFIG", *configPath)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("配置加载失败", "error", err)
		os.Exit(1)
	}

	app, err := newApp(cfg)
	if err != nil {
		logger.Error("ip2region 服务初始化失败", "error", err)
		os.Exit(1)
	}
	defer app.Close()

	if cfg.PIDFile != "" {
		if err := writePIDFile(cfg.PIDFile); err != nil {
			logger.Error("写入 PID 文件失败", "path", cfg.PIDFile, "error", err)
			os.Exit(1)
		}
		defer removePIDFile(cfg.PIDFile)
	}

	h := handler.New(app, cfg.MaxBatchSize)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.Health)
	mux.HandleFunc("GET /api/v1/ip/query", h.Query)
	mux.HandleFunc("POST /api/v1/ip/batch", h.Batch)

	server := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      withMiddlewares(mux, logger),
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}

	go func() {
		logger.Info("服务启动", "addr", cfg.HTTPAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("HTTP 服务异常退出", "error", err)
			os.Exit(1)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	for sig := range sigCh {
		switch sig {
		case syscall.SIGHUP:
			logger.Info("收到 SIGHUP，开始热加载 xdb")
			if err := app.Reload(); err != nil {
				logger.Error("xdb 热加载失败，继续沿用旧数据", "error", err)
			} else {
				logger.Info("xdb 热加载成功")
			}
		case syscall.SIGINT, syscall.SIGTERM:
			logger.Info("收到退出信号，开始优雅关停")
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := server.Shutdown(ctx); err != nil {
				logger.Error("优雅关停失败", "error", err)
			}
			logger.Info("服务已停止")
			return
		}
	}
}

func writePIDFile(path string) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644)
}

func removePIDFile(path string) {
	_ = os.Remove(path)
}

func withMiddlewares(next http.Handler, logger *slog.Logger) http.Handler {
	return recoverMiddleware(loggingMiddleware(next, logger))
}

func loggingMiddleware(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		logger.Info("access",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"remote", r.RemoteAddr,
		)
	})
}

func recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"code":    50000,
					"message": "服务器内部错误",
				})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}
