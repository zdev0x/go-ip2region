package ip2region

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/lionsoul2014/ip2region/binding/golang/service"

	"github.com/zdev0x/go-ip2region/config"
	"github.com/zdev0x/go-ip2region/model"
)

// Service 封装 ip2region 并发安全的查询服务，提供结构化单查与批量查询。
type Service struct {
	cfg  *config.Config
	core *service.Ip2Region
}

// New 依据配置构建查询服务。任一 xdb 路径为空时对应版本将被禁用。
func New(cfg *config.Config) (*Service, error) {
	policy, err := service.CachePolicyFromName(cfg.CachePolicy)
	if err != nil {
		return nil, fmt.Errorf("缓存策略配置错误: %w", err)
	}

	var v4Cfg, v6Cfg *service.Config
	if cfg.V4XdbPath != "" {
		v4Cfg, err = service.NewV4Config(policy, cfg.V4XdbPath, cfg.Searchers)
		if err != nil {
			return nil, fmt.Errorf("初始化 v4 配置失败: %w", err)
		}
	}
	if cfg.V6XdbPath != "" {
		v6Cfg, err = service.NewV6Config(policy, cfg.V6XdbPath, cfg.Searchers)
		if err != nil {
			return nil, fmt.Errorf("初始化 v6 配置失败: %w", err)
		}
	}

	core, err := service.NewIp2Region(v4Cfg, v6Cfg)
	if err != nil {
		return nil, fmt.Errorf("创建 ip2region 服务失败: %w", err)
	}

	return &Service{cfg: cfg, core: core}, nil
}

// Search 查询单个 IP，返回结构化归属地。非法 IP 在借 searcher 前快速失败。
func (s *Service) Search(_ context.Context, ip string) (*model.Region, error) {
	if net.ParseIP(ip) == nil {
		return nil, fmt.Errorf("非法 IP 地址: %s", ip)
	}
	raw, err := s.core.Search(ip)
	if err != nil {
		return nil, fmt.Errorf("查询 %s 失败: %w", ip, err)
	}
	return parseRegion(ip, raw), nil
}

// BatchSearch 批量查询，结果与入参顺序一致；单条失败不影响其他项。
// 并发度受 searcher 池大小限制，避免 goroutine 与底层池无限膨胀。
func (s *Service) BatchSearch(ctx context.Context, ips []string) []model.BatchItem {
	results := make([]model.BatchItem, len(ips))
	if len(ips) == 0 {
		return results
	}

	concurrency := len(ips)
	if s.cfg.Searchers > 0 && concurrency > s.cfg.Searchers {
		concurrency = s.cfg.Searchers
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i, ip := range ips {
		wg.Add(1)
		go func(idx int, ip string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			item := model.BatchItem{Region: model.Region{IP: ip}}
			region, err := s.Search(ctx, ip)
			if err != nil {
				item.Error = err.Error()
			} else {
				item.Region = *region
			}
			results[idx] = item
		}(i, ip)
	}
	wg.Wait()
	return results
}

// Close 释放底层查询资源，供优雅关停调用。
func (s *Service) Close() error {
	if s.core != nil {
		s.core.Close()
	}
	return nil
}

// parseRegion 解析原始串 Country|Province|City|ISP|iso-alpha2-code。
func parseRegion(ip, raw string) *model.Region {
	r := &model.Region{IP: ip, Raw: raw}
	const fields = 5
	vals := make([]string, fields)
	parts := strings.Split(raw, "|")
	for i := 0; i < fields; i++ {
		if i < len(parts) {
			vals[i] = normalizeField(parts[i])
		}
	}
	r.Country = vals[0]
	r.Province = vals[1]
	r.City = vals[2]
	r.ISP = vals[3]
	r.Code = vals[4]
	return r
}

// normalizeField 将 ip2region 占位符 "0" 归并为未知空串。
func normalizeField(s string) string {
	s = strings.TrimSpace(s)
	if s == "0" || s == "" {
		return ""
	}
	return s
}
