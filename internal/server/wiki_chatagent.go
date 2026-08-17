package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"aigoni/internal/agent"
	"aigoni/internal/content"
)

const (
	wikiAgentRunTTL          = 30 * time.Minute
	wikiAgentRunTimeout      = 10 * time.Minute
	wikiAgentCleanupInterval = time.Minute
)

var errWikiAgentConcurrencyLimit = errors.New("wiki agent concurrency limit reached")

type adminWikiChatRequest struct {
	Message string            `json:"message"`
	History []wikiChatMessage `json:"history"`
}

type wikiAgentRun struct {
	ID         string              `json:"id"`
	Kind       string              `json:"kind"`
	Status     string              `json:"status"`
	Message    string              `json:"message"`
	Error      string              `json:"error,omitempty"`
	Question   string              `json:"question,omitempty"`
	Answer     string              `json:"answer,omitempty"`
	AnswerHTML string              `json:"answer_html,omitempty"`
	Reasoning  string              `json:"reasoning,omitempty"`
	Sources    []string            `json:"sources,omitempty"`
	Files      []string            `json:"files,omitempty"`
	Events     []wikiAgentRunEvent `json:"events,omitempty"`
	CreatedAt  string              `json:"created_at"`
	UpdatedAt  string              `json:"updated_at"`
	ExpiresAt  time.Time           `json:"-"`
	Deadline   time.Time           `json:"-"`
	Cancel     context.CancelFunc  `json:"-"`
}

type wikiAgentRunEvent struct {
	Seq       int       `json:"seq"`
	Time      string    `json:"time"`
	Event     string    `json:"event"`
	Payload   any       `json:"payload"`
	CreatedAt time.Time `json:"-"`
}

func (s *Server) StartWikiAgentCleanup() {
	s.wikiAgentMu.Lock()
	if s.wikiAgentCleanupActive {
		s.wikiAgentMu.Unlock()
		return
	}
	s.wikiAgentCleanupActive = true
	s.wikiAgentMu.Unlock()

	go func() {
		ticker := time.NewTicker(wikiAgentCleanupInterval)
		defer ticker.Stop()
		for now := range ticker.C {
			s.cleanupWikiAgentRuns(now)
		}
	}()
}

func (s *Server) adminWikiAPIChat(w http.ResponseWriter, r *http.Request) {
	var request adminWikiChatRequest
	if !decodeAdminWikiJSON(w, r, &request, false) {
		return
	}
	message := strings.TrimSpace(request.Message)
	if message == "" {
		writeAdminErrorFields(w, http.StatusBadRequest, "validation_failed", "消息不能为空。", map[string]string{"message": "消息不能为空。"})
		return
	}
	if !s.wikiReady() {
		writeAdminError(w, http.StatusServiceUnavailable, "wiki_unavailable", "Wiki Agent LLM 尚未配置完成。")
		return
	}
	run, err := s.startWikiAgentRunWithKind("chat", message, request.History)
	if err != nil {
		writeWikiAgentStartError(w, err)
		return
	}
	w.Header().Set("Location", adminWikiAPIBasePath+"/runs/"+run.ID)
	writeAdminJSON(w, http.StatusAccepted, adminWikiAgentRunResponse(run))
}

func (s *Server) adminWikiAPIChatStream(w http.ResponseWriter, r *http.Request) {
	var request adminWikiChatRequest
	if !decodeAdminWikiJSON(w, r, &request, false) {
		return
	}
	message := strings.TrimSpace(request.Message)
	if message == "" {
		writeAdminErrorFields(w, http.StatusBadRequest, "validation_failed", "消息不能为空。", map[string]string{"message": "消息不能为空。"})
		return
	}
	if !s.wikiReady() {
		writeAdminError(w, http.StatusServiceUnavailable, "wiki_unavailable", "Wiki Agent LLM 尚未配置完成。")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeAdminError(w, http.StatusInternalServerError, "stream_unsupported", "当前连接不支持 SSE。")
		return
	}
	run, err := s.startWikiAgentRunWithKind("chat", message, request.History)
	if err != nil {
		writeWikiAgentStartError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	_ = s.writeWikiSSE(w, flusher, "step", wikiSSEPayload{Title: "Wiki Agent 已启动", Status: "running", Message: run.ID})
	s.streamWikiAgentRunEvents(r.Context(), w, flusher, run.ID, 0)
}

func writeWikiAgentStartError(w http.ResponseWriter, err error) {
	if errors.Is(err, errWikiAgentConcurrencyLimit) {
		writeAdminError(w, http.StatusConflict, "run_conflict", "Wiki Agent 并发已满，请稍后重试或取消运行中的任务。")
		return
	}
	writeAdminError(w, http.StatusInternalServerError, "agent_start_failed", err.Error())
}

func (s *Server) adminWikiAPIRun(w http.ResponseWriter, r *http.Request) {
	s.cleanupWikiAgentRuns(time.Now())
	run := s.getWikiAgentRun(strings.TrimSpace(r.PathValue("id")))
	if run == nil {
		writeAdminError(w, http.StatusNotFound, "run_not_found", "Wiki Agent 运行不存在。")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeAdminJSON(w, http.StatusOK, adminWikiAgentRunResponse(run))
}

func (s *Server) adminWikiAPIRunCancel(w http.ResponseWriter, r *http.Request) {
	run, canceled := s.cancelWikiAgentRun(strings.TrimSpace(r.PathValue("id")), "Wiki Agent 已取消。", "Wiki Agent 运行已取消。")
	if run == nil {
		writeAdminError(w, http.StatusNotFound, "run_not_found", "Wiki Agent 运行不存在。")
		return
	}
	if !canceled {
		writeAdminError(w, http.StatusConflict, "run_not_active", "Wiki Agent 已结束，无法取消。")
		return
	}
	writeAdminJSON(w, http.StatusOK, adminWikiAgentRunResponse(run))
}

func (s *Server) adminWikiAPIRunEvents(w http.ResponseWriter, r *http.Request) {
	s.cleanupWikiAgentRuns(time.Now())
	runID := strings.TrimSpace(r.PathValue("id"))
	if s.getWikiAgentRun(runID) == nil {
		writeAdminError(w, http.StatusNotFound, "run_not_found", "Wiki Agent 运行不存在。")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeAdminError(w, http.StatusInternalServerError, "stream_unsupported", "当前连接不支持 SSE。")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	from := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("from")); raw != "" {
		_, _ = fmt.Sscanf(raw, "%d", &from)
	}
	s.streamWikiAgentRunEvents(r.Context(), w, flusher, runID, from)
}

func (s *Server) startWikiAgentRunWithKind(kind, message string, history []wikiChatMessage) (*wikiAgentRun, error) {
	return s.startWikiAgentRun(s.newWikiAgentRunner, kind, message, history)
}

// startReadOnlyWikiAskRun starts a machine-facing read-only ask run. It uses
// the dedicated read-only runner and the "ask" kind so the browser-admin
// write-capable chat runner and run kind stay isolated.
func (s *Server) startReadOnlyWikiAskRun(message string, history []wikiChatMessage) (*wikiAgentRun, error) {
	return s.startWikiAgentRun(s.newReadOnlyWikiAgentRunner, "ask", message, history)
}

func (s *Server) startWikiAgentRun(newRunner func(context.Context) (agent.Runner, error), kind, message string, history []wikiChatMessage) (*wikiAgentRun, error) {
	now := time.Now()
	s.cleanupWikiAgentRuns(now)
	kind = strings.TrimSpace(kind)

	s.wikiAgentMu.Lock()
	defer s.wikiAgentMu.Unlock()

	// Admin Chat 与机器 Ask 共用同一并发池：统计所有 pending/running run，
	// 达到 AIGONI_WIKI_AGENT_CONCURRENCY 上限时拒绝新提交。kind 不再做单槽互斥。
	// 未配置时按默认 5 兜底，避免直接构造 Env{}（绕过 LoadEnv）导致并发数 0 全量拒绝。
	limit := s.env.WikiAgentConcurrency
	if limit < 1 {
		limit = 5
	}
	s.wikiMu.Lock()
	if s.countActiveWikiAgentRunsLocked() >= limit {
		s.wikiMu.Unlock()
		return nil, errWikiAgentConcurrencyLimit
	}
	s.wikiMu.Unlock()

	runner, err := newRunner(context.Background())
	if err != nil {
		return nil, err
	}
	id, err := newWikiAgentRunID()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), wikiAgentRunTimeout)
	run := &wikiAgentRun{
		ID:        id,
		Kind:      kind,
		Status:    "pending",
		Message:   "Wiki Agent 已加入运行队列。",
		Question:  message,
		CreatedAt: now.Format(time.RFC3339),
		UpdatedAt: now.Format(time.RFC3339),
		ExpiresAt: now.Add(wikiAgentRunTTL),
		Deadline:  now.Add(wikiAgentRunTimeout),
		Cancel:    cancel,
	}
	s.wikiMu.Lock()
	s.wikiAgentRuns[run.ID] = run
	s.wikiMu.Unlock()
	go func() {
		defer cancel()
		s.runWikiAgent(ctx, runner, run.ID, message, history)
	}()
	return s.getWikiAgentRun(run.ID), nil
}

func (s *Server) runWikiAgent(ctx context.Context, runner agent.Runner, runID, message string, history []wikiChatMessage) {
	if !s.markWikiAgentRunRunning(runID) {
		return
	}
	result, err := runner.Run(ctx, agent.RunRequest{Message: message, History: toAgentHistory(history)}, agent.EventSinkFunc(func(event agent.Event) {
		s.handleWikiAgentEvent(runID, event)
	}))
	if err != nil {
		s.finishWikiAgentRunError(runID, "Wiki Agent 运行失败。", err.Error())
		return
	}
	answer := strings.TrimSpace(result.Answer)
	html, renderErr := content.RenderMarkdown(answer)
	if renderErr != nil {
		s.finishWikiAgentRunError(runID, "Wiki Agent 答案渲染失败。", renderErr.Error())
		return
	}
	s.finishWikiAgentRunDone(runID, message, answer, html, result)
}

func (s *Server) handleWikiAgentEvent(runID string, event agent.Event) {
	switch event.Type {
	case "delta":
		s.wikiMu.Lock()
		if run := s.wikiAgentRuns[runID]; run != nil && wikiAgentRunActive(run.Status) {
			run.Answer = event.Answer
			run.UpdatedAt = time.Now().Format(time.RFC3339)
			run.ExpiresAt = time.Now().Add(wikiAgentRunTTL)
			s.appendWikiAgentRunEventLocked(run, "delta", map[string]any{"delta": event.Delta, "answer": event.Answer})
		}
		s.wikiMu.Unlock()
	case "tool":
		s.appendWikiAgentRunEventIfActive(runID, "tool", wikiSSEPayload{
			Type:       event.Type,
			Name:       event.Name,
			Tool:       event.Tool,
			Status:     event.Status,
			Summary:    event.Summary,
			Args:       event.Args,
			Result:     event.Result,
			StartedAt:  event.StartedAt,
			DurationMS: event.DurationMS,
			Error:      event.Error,
			Message:    event.Message,
		})
	case "step":
		s.appendWikiAgentRunEventIfActive(runID, "step", wikiSSEPayload{Title: event.Title, Status: event.Status, Message: event.Message})
	}
}

func (s *Server) newWikiAgentRunner(ctx context.Context) (agent.Runner, error) {
	if s.wikiAgentRunner != nil {
		return s.wikiAgentRunner, nil
	}
	runner, err := agent.New(ctx, agent.Config{
		Root:      s.root,
		APIKey:    s.env.WikiAPIKey,
		BaseURL:   wikiAgentBaseURL(s.env.WikiBaseURL),
		Model:     s.env.WikiModel,
		WriteLock: &s.wikiWriteMu,
	})
	if err != nil {
		return nil, err
	}
	s.wikiAgentRunner = runner
	return runner, nil
}

// newReadOnlyWikiAgentRunner builds the dedicated read-only runner for machine
// Wiki Ask runs. It has no write tool and no wiki write lock, which is the
// code-level boundary that prevents external API clients from modifying the Wiki.
func (s *Server) newReadOnlyWikiAgentRunner(ctx context.Context) (agent.Runner, error) {
	if s.wikiAgentAskRunner != nil {
		return s.wikiAgentAskRunner, nil
	}
	runner, err := agent.New(ctx, agent.Config{
		Root:     s.root,
		APIKey:   s.env.WikiAPIKey,
		BaseURL:  wikiAgentBaseURL(s.env.WikiBaseURL),
		Model:    s.env.WikiModel,
		ReadOnly: true,
	})
	if err != nil {
		return nil, err
	}
	s.wikiAgentAskRunner = runner
	return runner, nil
}

func wikiAgentBaseURL(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" || strings.HasSuffix(base, "/v1") {
		return base
	}
	return base + "/v1"
}

func toAgentHistory(history []wikiChatMessage) []agent.ChatMessage {
	out := make([]agent.ChatMessage, 0, len(history))
	for _, item := range sanitizeWikiChatHistory(history) {
		out = append(out, agent.ChatMessage{Role: item.Role, Content: item.Content})
	}
	return out
}

func (s *Server) getWikiAgentRun(id string) *wikiAgentRun {
	s.wikiMu.Lock()
	defer s.wikiMu.Unlock()
	run := s.wikiAgentRuns[id]
	if run == nil {
		return nil
	}
	return cloneWikiAgentRun(run)
}

func (s *Server) markWikiAgentRunRunning(id string) bool {
	s.wikiMu.Lock()
	defer s.wikiMu.Unlock()
	run := s.wikiAgentRuns[id]
	if run == nil || !wikiAgentRunActive(run.Status) {
		return false
	}
	run.Status = "running"
	run.Message = "Wiki Agent 正在运行。"
	run.UpdatedAt = time.Now().Format(time.RFC3339)
	run.ExpiresAt = time.Now().Add(wikiAgentRunTTL)
	s.appendWikiAgentRunEventLocked(run, "step", wikiSSEPayload{Title: "开始 Wiki Agent", Status: "running"})
	return true
}

func (s *Server) finishWikiAgentRunDone(id, question, answer, html string, result *agent.RunResult) {
	s.wikiMu.Lock()
	defer s.wikiMu.Unlock()
	run := s.wikiAgentRuns[id]
	if run == nil || !wikiAgentRunActive(run.Status) {
		return
	}
	run.Status = "done"
	run.Message = "Wiki Agent 运行完成。"
	run.Error = ""
	run.Answer = answer
	run.AnswerHTML = html
	run.Reasoning = result.Reasoning
	run.Sources = append([]string(nil), result.Sources...)
	run.Files = append([]string(nil), result.Files...)
	run.Cancel = nil
	run.UpdatedAt = time.Now().Format(time.RFC3339)
	run.ExpiresAt = time.Now().Add(wikiAgentRunTTL)
	s.appendWikiAgentRunEventLocked(run, "done", map[string]any{"question": question, "answer": answer, "answer_html": html, "reasoning": result.Reasoning, "sources": result.Sources, "files": result.Files})
}

func (s *Server) finishWikiAgentRunError(id, message, errMsg string) {
	s.wikiMu.Lock()
	defer s.wikiMu.Unlock()
	run := s.wikiAgentRuns[id]
	if run == nil || !wikiAgentRunActive(run.Status) {
		return
	}
	run.Status = "error"
	run.Message = message
	run.Error = errMsg
	run.Cancel = nil
	run.UpdatedAt = time.Now().Format(time.RFC3339)
	run.ExpiresAt = time.Now().Add(wikiAgentRunTTL)
	s.appendWikiAgentRunEventLocked(run, "error", wikiSSEPayload{Message: errMsg})
}

func (s *Server) cancelWikiAgentRun(id, message, errMsg string) (*wikiAgentRun, bool) {
	var cancel context.CancelFunc
	s.wikiMu.Lock()
	run := s.wikiAgentRuns[id]
	if run == nil {
		s.wikiMu.Unlock()
		return nil, false
	}
	if !wikiAgentRunActive(run.Status) {
		clone := cloneWikiAgentRun(run)
		s.wikiMu.Unlock()
		return clone, false
	}
	run.Status = "error"
	run.Message = message
	run.Error = errMsg
	cancel = run.Cancel
	run.Cancel = nil
	run.UpdatedAt = time.Now().Format(time.RFC3339)
	run.ExpiresAt = time.Now().Add(wikiAgentRunTTL)
	s.appendWikiAgentRunEventLocked(run, "error", wikiSSEPayload{Message: errMsg})
	clone := cloneWikiAgentRun(run)
	s.wikiMu.Unlock()
	if cancel != nil {
		cancel()
	}
	return clone, true
}

func (s *Server) appendWikiAgentRunEvent(id, event string, payload any) {
	s.wikiMu.Lock()
	defer s.wikiMu.Unlock()
	if run := s.wikiAgentRuns[id]; run != nil {
		s.appendWikiAgentRunEventLocked(run, event, payload)
	}
}

func (s *Server) appendWikiAgentRunEventIfActive(id, event string, payload any) {
	s.wikiMu.Lock()
	defer s.wikiMu.Unlock()
	if run := s.wikiAgentRuns[id]; run != nil && wikiAgentRunActive(run.Status) {
		s.appendWikiAgentRunEventLocked(run, event, payload)
	}
}

func (s *Server) appendWikiAgentRunEventLocked(run *wikiAgentRun, event string, payload any) {
	run.Events = append(run.Events, wikiAgentRunEvent{Seq: len(run.Events) + 1, Time: time.Now().Format(time.RFC3339), Event: event, Payload: payload, CreatedAt: time.Now()})
	if len(run.Events) > 500 {
		run.Events = append([]wikiAgentRunEvent(nil), run.Events[len(run.Events)-500:]...)
		for i := range run.Events {
			run.Events[i].Seq = i + 1
		}
	}
	run.UpdatedAt = time.Now().Format(time.RFC3339)
	run.ExpiresAt = time.Now().Add(wikiAgentRunTTL)
}

func (s *Server) streamWikiAgentRunEvents(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, runID string, from int) {
	seen := from
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		run := s.getWikiAgentRun(runID)
		if run == nil {
			_ = s.writeWikiSSE(w, flusher, "error", wikiSSEPayload{Message: "Wiki Agent 运行不存在。"})
			return
		}
		for _, event := range run.Events {
			if event.Seq <= seen {
				continue
			}
			_ = s.writeWikiSSE(w, flusher, event.Event, event.Payload)
			seen = event.Seq
		}
		if wikiAgentRunTerminal(run.Status) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) cleanupWikiAgentRuns(now time.Time) {
	var cancels []context.CancelFunc
	s.wikiMu.Lock()
	for id, run := range s.wikiAgentRuns {
		if wikiAgentRunActive(run.Status) {
			if !run.Deadline.IsZero() && !now.Before(run.Deadline) {
				run.Status = "error"
				run.Message = "Wiki Agent 运行超时。"
				run.Error = "Wiki Agent 运行超时。"
				if run.Cancel != nil {
					cancels = append(cancels, run.Cancel)
					run.Cancel = nil
				}
				run.UpdatedAt = now.Format(time.RFC3339)
				run.ExpiresAt = now.Add(wikiAgentRunTTL)
				s.appendWikiAgentRunEventLocked(run, "error", wikiSSEPayload{Message: run.Error})
			}
			continue
		}
		if !now.Before(run.ExpiresAt) {
			delete(s.wikiAgentRuns, id)
		}
	}
	s.wikiMu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

// countActiveWikiAgentRunsLocked 统计所有 pending/running 的 Wiki Agent run，
// Admin Chat 与机器 Ask 共用同一并发额度。调用方必须持有 s.wikiMu。
func (s *Server) countActiveWikiAgentRunsLocked() int {
	count := 0
	for _, run := range s.wikiAgentRuns {
		if wikiAgentRunActive(run.Status) {
			count++
		}
	}
	return count
}

func wikiAgentRunActive(status string) bool {
	return status == "pending" || status == "running"
}

func wikiAgentRunTerminal(status string) bool {
	return status == "done" || status == "error"
}

func newWikiAgentRunID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func cloneWikiAgentRun(run *wikiAgentRun) *wikiAgentRun {
	clone := *run
	clone.Sources = append([]string(nil), run.Sources...)
	clone.Files = append([]string(nil), run.Files...)
	clone.Events = append([]wikiAgentRunEvent(nil), run.Events...)
	clone.Cancel = nil
	return &clone
}

func adminWikiAgentRunResponse(run *wikiAgentRun) *wikiAgentRun {
	if run == nil {
		return nil
	}
	clone := cloneWikiAgentRun(run)
	switch clone.Status {
	case "done":
		clone.Status = "completed"
	case "error":
		clone.Status = "failed"
	case "pending", "running":
	default:
		clone.Status = "failed"
	}
	return clone
}
