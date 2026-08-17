package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"aigoni/internal/agent"
)

func TestAdminWikiAPIRequiresSessionAndCSRF(t *testing.T) {
	srv := testServer(t)
	handler := adminWikiAPITestHandler(srv)

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, adminWikiAPIBasePath+"/status", nil))
	if unauthorized.Code != http.StatusUnauthorized || !strings.Contains(unauthorized.Body.String(), `"code":"unauthenticated"`) {
		t.Fatalf("unauthorized status = %d, body = %s", unauthorized.Code, unauthorized.Body.String())
	}

	cookie, _ := adminWikiAPILogin(t, srv)
	forbidden := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, adminWikiAPIBasePath+"/render", strings.NewReader(`{"markdown":"# Test"}`))
	request.AddCookie(cookie)
	handler.ServeHTTP(forbidden, request)
	if forbidden.Code != http.StatusForbidden || !strings.Contains(forbidden.Body.String(), `"code":"csrf_failed"`) {
		t.Fatalf("forbidden status = %d, body = %s", forbidden.Code, forbidden.Body.String())
	}
}

func TestAdminWikiAPIStatusAndDocuments(t *testing.T) {
	srv := testServer(t)
	writeFile(t, filepath.Join(srv.root, "wiki/index.md"), "# Wiki Index\n")
	writeFile(t, filepath.Join(srv.root, "wiki/concepts/test.md"), `---
title: "Test"
type: "concept"
status: "active"
updated_at: "2026-08-03"
sources: []
source_count: 0
---
# Test

## 摘要

Summary.
`)
	writeFile(t, filepath.Join(srv.root, "wiki/.backups/one/concepts/hidden.md"), "hidden")
	handler := adminWikiAPITestHandler(srv)
	cookie, _ := adminWikiAPILogin(t, srv)

	status := adminWikiAPIRequest(t, handler, cookie, "", http.MethodGet, adminWikiAPIBasePath+"/status", "")
	if status.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", status.Code, status.Body.String())
	}
	var statusBody struct {
		Ready bool `json:"ready"`
	}
	if err := json.NewDecoder(status.Body).Decode(&statusBody); err != nil {
		t.Fatal(err)
	}
	if statusBody.Ready {
		t.Fatalf("status body = %+v", statusBody)
	}

	list := adminWikiAPIRequest(t, handler, cookie, "", http.MethodGet, adminWikiAPIBasePath+"/documents", "")
	if list.Code != http.StatusOK || strings.Contains(list.Body.String(), "hidden.md") || !strings.Contains(list.Body.String(), "wiki/concepts/test.md") {
		t.Fatalf("documents status = %d, body = %s", list.Code, list.Body.String())
	}

	content := adminWikiAPIRequest(t, handler, cookie, "", http.MethodGet, adminWikiAPIBasePath+"/documents/content?path=wiki/concepts/test.md", "")
	if content.Code != http.StatusOK || !strings.Contains(content.Body.String(), `"html"`) || !strings.Contains(content.Body.String(), `"title":"Test"`) {
		t.Fatalf("content status = %d, body = %s", content.Code, content.Body.String())
	}

	blocked := adminWikiAPIRequest(t, handler, cookie, "", http.MethodGet, adminWikiAPIBasePath+"/documents/content?path=../config.yaml", "")
	if blocked.Code != http.StatusNotFound {
		t.Fatalf("blocked status = %d, body = %s", blocked.Code, blocked.Body.String())
	}
}

func TestAdminWikiAPIRenderAndBackups(t *testing.T) {
	srv := testServer(t)
	writeFile(t, filepath.Join(srv.root, "wiki/.backups/one/index.md"), "backup")
	handler := adminWikiAPITestHandler(srv)
	cookie, csrf := adminWikiAPILogin(t, srv)

	render := adminWikiAPIRequest(t, handler, cookie, csrf, http.MethodPost, adminWikiAPIBasePath+"/render", `{"markdown":"# Hello"}`)
	if render.Code != http.StatusOK || !strings.Contains(render.Body.String(), "Hello") {
		t.Fatalf("render status = %d, body = %s", render.Code, render.Body.String())
	}

	removed := adminWikiAPIRequest(t, handler, cookie, csrf, http.MethodDelete, adminWikiAPIBasePath+"/backups", "")
	if removed.Code != http.StatusOK || !strings.Contains(removed.Body.String(), `"removed":1`) {
		t.Fatalf("delete status = %d, body = %s", removed.Code, removed.Body.String())
	}
}

func TestAdminWikiAPIChatRunAndEvents(t *testing.T) {
	srv := testServer(t)
	configureFakeWikiAgent(srv)
	handler := adminWikiAPITestHandler(srv)
	cookie, csrf := adminWikiAPILogin(t, srv)

	response := adminWikiAPIRequest(t, handler, cookie, csrf, http.MethodPost, adminWikiAPIBasePath+"/chat", `{"message":"检查 Wiki"}`)
	if response.Code != http.StatusAccepted || !strings.Contains(response.Header().Get("Location"), "/wiki/runs/") {
		t.Fatalf("chat status = %d, location = %q, body = %s", response.Code, response.Header().Get("Location"), response.Body.String())
	}
	var accepted wikiAgentRun
	if err := json.NewDecoder(response.Body).Decode(&accepted); err != nil {
		t.Fatal(err)
	}
	if len(accepted.ID) != 32 {
		t.Fatalf("run id = %q, want 32 hex chars", accepted.ID)
	}
	waitWikiAgentRunStatus(t, srv, accepted.ID, "done")
	waitWikiAgentRunEvent(t, srv, accepted.ID, "done")

	run := adminWikiAPIRequest(t, handler, cookie, "", http.MethodGet, adminWikiAPIBasePath+"/runs/"+accepted.ID, "")
	if run.Code != http.StatusOK || !strings.Contains(run.Body.String(), `"status":"completed"`) || !strings.Contains(run.Body.String(), `"answer_html"`) || !strings.Contains(run.Body.String(), `"files":["wiki/syntheses/test.md"]`) {
		t.Fatalf("run status = %d, body = %s", run.Code, run.Body.String())
	}
	events := adminWikiAPIRequest(t, handler, cookie, "", http.MethodGet, adminWikiAPIBasePath+"/runs/"+accepted.ID+"/events", "")
	body := events.Body.String()
	if events.Code != http.StatusOK || !strings.Contains(body, "event: done") || !strings.Contains(body, `"tool":"read"`) || !strings.Contains(body, `"path":"wiki/index.md"`) || strings.Contains(body, `"output"`) || strings.Contains(body, "secret markdown") {
		t.Fatalf("events status = %d, body = %s", events.Code, body)
	}
}

func TestAdminWikiAPIChatStream(t *testing.T) {
	srv := testServer(t)
	configureFakeWikiAgent(srv)
	handler := adminWikiAPITestHandler(srv)
	cookie, csrf := adminWikiAPILogin(t, srv)

	response := adminWikiAPIRequest(t, handler, cookie, csrf, http.MethodPost, adminWikiAPIBasePath+"/chat:stream", `{"message":"回答"}`)
	if response.Code != http.StatusOK || !strings.Contains(response.Header().Get("Content-Type"), "text/event-stream") || !strings.Contains(response.Body.String(), "event: done") {
		t.Fatalf("stream status = %d, content-type = %q, body = %s", response.Code, response.Header().Get("Content-Type"), response.Body.String())
	}
}

func TestAdminWikiAPIChatUsesAgentWithHistory(t *testing.T) {
	srv := testServer(t)
	fake := configureFakeWikiAgent(srv)
	handler := adminWikiAPITestHandler(srv)
	cookie, csrf := adminWikiAPILogin(t, srv)

	body := `{"message":"继续说","history":[{"role":"user","content":"上一轮问题"},{"role":"assistant","content":"上一轮回答"}]}`
	response := adminWikiAPIRequest(t, handler, cookie, csrf, http.MethodPost, adminWikiAPIBasePath+"/chat", body)
	if response.Code != http.StatusAccepted || !strings.Contains(response.Body.String(), `"kind":"chat"`) {
		t.Fatalf("chat status = %d, body = %s", response.Code, response.Body.String())
	}
	select {
	case req := <-fake.requests:
		if req.Message != "继续说" || len(req.History) != 2 || req.History[0].Content != "上一轮问题" {
			t.Fatalf("agent request = %+v", req)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("agent request was not sent")
	}
}

func TestAdminWikiAPIChatAllowsMultipleConcurrentRuns(t *testing.T) {
	srv := testServer(t)
	fake := configureBlockingWikiAgent(srv)
	handler := adminWikiAPITestHandler(srv)
	cookie, csrf := adminWikiAPILogin(t, srv)

	// 并发未满时，同一 kind 允许同时存在多个 chat run。
	first := adminWikiAPIRequest(t, handler, cookie, csrf, http.MethodPost, adminWikiAPIBasePath+"/chat", `{"message":"first"}`)
	if first.Code != http.StatusAccepted {
		t.Fatalf("first status = %d, body = %s", first.Code, first.Body.String())
	}
	second := adminWikiAPIRequest(t, handler, cookie, csrf, http.MethodPost, adminWikiAPIBasePath+"/chat", `{"message":"second"}`)
	if second.Code != http.StatusAccepted {
		t.Fatalf("second status = %d, body = %s", second.Code, second.Body.String())
	}
	close(fake.release)
}

func TestAdminWikiAPIChatEnforcesConcurrencyLimit(t *testing.T) {
	srv := testServer(t)
	srv.env.WikiAgentConcurrency = 1
	fake := configureBlockingWikiAgent(srv)
	handler := adminWikiAPITestHandler(srv)
	cookie, csrf := adminWikiAPILogin(t, srv)

	first := adminWikiAPIRequest(t, handler, cookie, csrf, http.MethodPost, adminWikiAPIBasePath+"/chat", `{"message":"first"}`)
	if first.Code != http.StatusAccepted {
		t.Fatalf("first status = %d, body = %s", first.Code, first.Body.String())
	}
	<-fake.started
	second := adminWikiAPIRequest(t, handler, cookie, csrf, http.MethodPost, adminWikiAPIBasePath+"/chat", `{"message":"second"}`)
	if second.Code != http.StatusConflict || !strings.Contains(second.Body.String(), `"code":"run_conflict"`) {
		t.Fatalf("second status = %d, body = %s", second.Code, second.Body.String())
	}
	close(fake.release)
}

func TestAdminWikiAndAskShareConcurrencyPool(t *testing.T) {
	t.Run("chat fills ask rejected", func(t *testing.T) {
		srv := testServer(t)
		srv.env.WikiAgentConcurrency = 1
		srv.env.AigoniAPIKey = "test-key"
		enableMachineWikiAsk(srv)
		fake := configureBlockingWikiAgent(srv)
		handler := adminWikiAPITestHandler(srv)
		cookie, csrf := adminWikiAPILogin(t, srv)

		first := adminWikiAPIRequest(t, handler, cookie, csrf, http.MethodPost, adminWikiAPIBasePath+"/chat", `{"message":"first"}`)
		if first.Code != http.StatusAccepted {
			t.Fatalf("chat status = %d, body = %s", first.Code, first.Body.String())
		}
		<-fake.started
		ask := performMachineRequest(t, srv, http.MethodPost, adminWikiAPIBasePath+"/ask:api", "test-key", `{"message":"ask"}`)
		if ask.Code != http.StatusConflict || !strings.Contains(ask.Body.String(), `"error":"wiki agent concurrency limit reached"`) {
			t.Fatalf("ask status = %d, body = %s", ask.Code, ask.Body.String())
		}
		close(fake.release)
	})

	t.Run("ask fills chat rejected", func(t *testing.T) {
		srv := testServer(t)
		srv.env.WikiAgentConcurrency = 1
		srv.env.AigoniAPIKey = "test-key"
		enableMachineWikiAsk(srv)
		fake := configureBlockingWikiAgent(srv)

		ask := performMachineRequest(t, srv, http.MethodPost, adminWikiAPIBasePath+"/ask:api", "test-key", `{"message":"ask"}`)
		if ask.Code != http.StatusAccepted {
			t.Fatalf("ask status = %d, body = %s", ask.Code, ask.Body.String())
		}
		<-fake.started
		handler := adminWikiAPITestHandler(srv)
		cookie, csrf := adminWikiAPILogin(t, srv)
		chat := adminWikiAPIRequest(t, handler, cookie, csrf, http.MethodPost, adminWikiAPIBasePath+"/chat", `{"message":"chat"}`)
		if chat.Code != http.StatusConflict || !strings.Contains(chat.Body.String(), `"code":"run_conflict"`) {
			t.Fatalf("chat status = %d, body = %s", chat.Code, chat.Body.String())
		}
		close(fake.release)
	})
}

func TestAdminWikiAPIRunCancel(t *testing.T) {
	srv := testServer(t)
	fake := configureBlockingWikiAgent(srv)
	handler := adminWikiAPITestHandler(srv)
	cookie, csrf := adminWikiAPILogin(t, srv)

	response := adminWikiAPIRequest(t, handler, cookie, csrf, http.MethodPost, adminWikiAPIBasePath+"/chat", `{"message":"cancel me"}`)
	if response.Code != http.StatusAccepted {
		t.Fatalf("chat status = %d, body = %s", response.Code, response.Body.String())
	}
	var accepted wikiAgentRun
	if err := json.NewDecoder(response.Body).Decode(&accepted); err != nil {
		t.Fatal(err)
	}
	<-fake.started
	cancel := adminWikiAPIRequest(t, handler, cookie, csrf, http.MethodPost, adminWikiAPIBasePath+"/runs/"+accepted.ID+"/cancel", "")
	if cancel.Code != http.StatusOK || !strings.Contains(cancel.Body.String(), `"status":"failed"`) {
		t.Fatalf("cancel status = %d, body = %s", cancel.Code, cancel.Body.String())
	}
	select {
	case <-fake.canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("agent context was not canceled")
	}
	waitWikiAgentRunStatus(t, srv, accepted.ID, "error")
	waitWikiAgentRunEvent(t, srv, accepted.ID, "error")
}

func TestCleanupWikiAgentRunsCancelsExpiredActiveRun(t *testing.T) {
	srv := testServer(t)
	canceled := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-ctx.Done()
		close(canceled)
	}()
	now := time.Now()
	srv.wikiAgentRuns["expired"] = &wikiAgentRun{ID: "expired", Kind: "chat", Status: "running", Deadline: now.Add(-time.Second), ExpiresAt: now.Add(time.Hour), Cancel: cancel}

	srv.cleanupWikiAgentRuns(now)
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("expired run was not canceled")
	}
	run := srv.getWikiAgentRun("expired")
	if run == nil || run.Status != "error" || !hasWikiAgentRunEvent(run, "error") {
		t.Fatalf("run after cleanup = %+v", run)
	}
}

type fakeWikiAgentRunner struct {
	requests   chan agent.RunRequest
	started    chan struct{}
	release    chan struct{}
	canceled   chan struct{}
	startOnce  sync.Once
	cancelOnce sync.Once
}

func (f *fakeWikiAgentRunner) Run(ctx context.Context, req agent.RunRequest, sink agent.EventSink) (*agent.RunResult, error) {
	f.requests <- req
	if f.started != nil {
		f.startOnce.Do(func() { close(f.started) })
	}
	if f.release != nil {
		select {
		case <-f.release:
		case <-ctx.Done():
			if f.canceled != nil {
				f.cancelOnce.Do(func() { close(f.canceled) })
			}
			return nil, ctx.Err()
		}
	}
	if sink != nil {
		durationMS := int64(3)
		sink.Emit(agent.Event{
			Type:       "tool",
			Name:       "read",
			Tool:       "read",
			Status:     "done",
			Summary:    "读取知识文件",
			Args:       map[string]any{"path": "wiki/index.md"},
			Result:     map[string]any{"bytes": 15},
			StartedAt:  "2026-08-10T09:18:21+08:00",
			DurationMS: &durationMS,
		})
		sink.Emit(agent.Event{Type: "delta", Delta: "完成", Answer: "完成"})
	}
	return &agent.RunResult{Answer: "完成", Sources: []string{"wiki/index.md"}, Files: []string{"wiki/syntheses/test.md"}}, nil
}

func configureFakeWikiAgent(srv *Server) *fakeWikiAgentRunner {
	srv.env.WikiAPIKey = "test-key"
	srv.env.WikiBaseURL = "http://example.invalid/v1"
	srv.env.WikiModel = "test-model"
	fake := &fakeWikiAgentRunner{requests: make(chan agent.RunRequest, 4)}
	srv.wikiAgentRunner = fake
	srv.wikiAgentAskRunner = fake
	return fake
}

func configureBlockingWikiAgent(srv *Server) *fakeWikiAgentRunner {
	fake := configureFakeWikiAgent(srv)
	fake.started = make(chan struct{})
	fake.release = make(chan struct{})
	fake.canceled = make(chan struct{})
	return fake
}

func waitWikiAgentRunStatus(t *testing.T, srv *Server, id, status string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		run := srv.getWikiAgentRun(id)
		if run != nil && run.Status == status {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	run := srv.getWikiAgentRun(id)
	if run == nil {
		t.Fatalf("run %s missing", id)
	}
	t.Fatalf("run %s status = %s, want %s, error = %s", id, run.Status, status, run.Error)
}

func waitWikiAgentRunEvent(t *testing.T, srv *Server, id, event string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		run := srv.getWikiAgentRun(id)
		if run != nil && hasWikiAgentRunEvent(run, event) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	run := srv.getWikiAgentRun(id)
	t.Fatalf("run %s missing event %q: %+v", id, event, run)
}

func hasWikiAgentRunEvent(run *wikiAgentRun, event string) bool {
	for _, item := range run.Events {
		if item.Event == event {
			return true
		}
	}
	return false
}

func adminWikiAPITestHandler(srv *Server) http.Handler {
	mux := http.NewServeMux()
	srv.registerAdminWikiAPIRoutes(mux)
	return mux
}

func adminWikiAPILogin(t *testing.T, srv *Server) (*http.Cookie, string) {
	t.Helper()
	response := httptest.NewRecorder()
	if !srv.auth.Login(response, "10.0.0.1:1234", "secret") {
		t.Fatal("login failed")
	}
	cookies := response.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("session cookie missing")
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(cookies[0])
	session, ok := srv.auth.Session(request)
	if !ok {
		t.Fatal("session missing")
	}
	return cookies[0], session.CSRFToken
}

func adminWikiAPIRequest(t *testing.T, handler http.Handler, cookie *http.Cookie, csrf, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.AddCookie(cookie)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if csrf != "" {
		request.Header.Set("X-CSRF-Token", csrf)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
