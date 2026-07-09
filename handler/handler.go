package handler

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/zdev0x/go-ip2region/model"
)

// Searcher 为查询能力的抽象，便于在底层服务热加载（SIGHUP）时无缝替换实现。
type Searcher interface {
	Search(ctx context.Context, ip string) (*model.Region, error)
	BatchSearch(ctx context.Context, ips []string) []model.BatchItem
}

// Handler 持有查询服务与批量上限，处理 HTTP 请求。
type Handler struct {
	svc      Searcher
	maxBatch int
}

func New(svc Searcher, maxBatch int) *Handler {
	return &Handler{svc: svc, maxBatch: maxBatch}
}

// Health 存活探针，不依赖 xdb。
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Query 处理 GET /api/v1/ip/query?ip=...
func (h *Handler) Query(w http.ResponseWriter, r *http.Request) {
	ip := strings.TrimSpace(r.URL.Query().Get("ip"))
	if ip == "" {
		writeError(w, http.StatusBadRequest, 40001, "缺少查询参数 ip")
		return
	}
	if net.ParseIP(ip) == nil {
		writeError(w, http.StatusBadRequest, 40002, "非法 IP 地址: "+ip)
		return
	}

	region, err := h.svc.Search(r.Context(), ip)
	if err != nil {
		if strings.Contains(err.Error(), "非法 IP") {
			writeError(w, http.StatusBadRequest, 40002, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, 50001, "查询失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, region)
}

// Batch 处理 POST /api/v1/ip/batch
func (h *Handler) Batch(w http.ResponseWriter, r *http.Request) {
	var req model.BatchRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, 40003, "请求体解析失败: "+err.Error())
		return
	}
	if len(req.IPs) == 0 {
		writeError(w, http.StatusBadRequest, 40004, "ips 不能为空")
		return
	}
	if len(req.IPs) > h.maxBatch {
		writeError(w, http.StatusBadRequest, 40005, "批量查询数量超出上限: "+strconv.Itoa(h.maxBatch))
		return
	}

	results := h.svc.BatchSearch(r.Context(), req.IPs)
	writeJSON(w, http.StatusOK, model.BatchResponse{
		Count:   len(results),
		Results: results,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status, code int, msg string) {
	writeJSON(w, status, model.ErrorResponse{Code: code, Message: msg})
}
