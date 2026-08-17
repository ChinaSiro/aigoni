package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

// rpmLimiter 是线程安全的每分钟请求计数限流器，按窗口重置计数。
type rpmLimiter struct {
	mu sync.Mutex
	// windowStart 是当前计数窗口的起点，count 是窗口内已放行的请求数。
	windowStart time.Time
	count       int
}

func newRPMLimiter() *rpmLimiter {
	return &rpmLimiter{}
}

// allow 返回当前窗口内是否允许再放行一个请求。limit <= 0 表示不限制。
// now 由调用方传入，便于测试注入固定时间。
func (l *rpmLimiter) allow(now time.Time, limit int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if limit <= 0 {
		return true
	}
	if l.windowStart.IsZero() || now.Sub(l.windowStart) >= time.Minute {
		l.windowStart = now
		l.count = 0
	}
	if l.count >= limit {
		return false
	}
	l.count++
	return true
}

// adminWikiAPIAskMachine POST /api/admin/v1/wiki/ask:api.
// Machine-facing read-only Wiki Ask: authenticated with AIGONI_API_KEY
// (X-API-Key or Authorization: Bearer), no Session Cookie and no CSRF.
// Runs use the read-only agent runner which has no write tool.
func (s *Server) adminWikiAPIAskMachine(w http.ResponseWriter, r *http.Request) {
	if !s.env.WikiAskAPIEnabled {
		writeAPIError(w, http.StatusForbidden, "wiki ask api disabled")
		return
	}
	var request adminWikiChatRequest
	if !decodeAPIRequest(w, r, &request) {
		return
	}
	message := strings.TrimSpace(request.Message)
	if message == "" {
		writeAPIError(w, http.StatusBadRequest, "message is required")
		return
	}
	if !s.wikiAskLimiter.allow(time.Now(), s.env.WikiAskAPIRPM) {
		writeAPIError(w, http.StatusTooManyRequests, "rate limit exceeded")
		return
	}
	if !s.wikiReady() {
		writeAPIError(w, http.StatusServiceUnavailable, "wiki unavailable")
		return
	}
	run, err := s.startReadOnlyWikiAskRun(message, request.History)
	if err != nil {
		writeWikiAgentStartMachineError(w, err)
		return
	}
	w.Header().Set("Location", adminWikiAPIBasePath+"/ask/runs/"+run.ID)
	writeAPIJSON(w, http.StatusAccepted, adminWikiAgentRunResponse(run))
}

// adminWikiAPIAskRunMachine GET /api/admin/v1/wiki/ask/runs/{id}.
// Polls a read-only Wiki Ask run. Only "ask" kind runs are visible here so
// machine clients cannot inspect write-capable browser-admin chat runs.
func (s *Server) adminWikiAPIAskRunMachine(w http.ResponseWriter, r *http.Request) {
	s.cleanupWikiAgentRuns(time.Now())
	run := s.getWikiAgentRun(strings.TrimSpace(r.PathValue("id")))
	if run == nil || run.Kind != "ask" {
		writeAPIError(w, http.StatusNotFound, "wiki ask run not found")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeAPIJSON(w, http.StatusOK, adminWikiAgentRunResponse(run))
}

func writeWikiAgentStartMachineError(w http.ResponseWriter, err error) {
	if errors.Is(err, errWikiAgentConcurrencyLimit) {
		writeAPIError(w, http.StatusConflict, "wiki agent concurrency limit reached")
		return
	}
	writeAPIError(w, http.StatusInternalServerError, err.Error())
}

// writeAPIJSON writes a successful machine API JSON response with the same
// content type behavior as the machine error envelope.
func writeAPIJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
