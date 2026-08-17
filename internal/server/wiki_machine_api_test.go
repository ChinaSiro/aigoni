package server

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func performMachineRequest(t *testing.T, srv *Server, method, target, apiKey, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res := httptest.NewRecorder()
	srv.Handler().ServeHTTP(res, req)
	return res
}

// enableMachineWikiAsk 开启机器只读 Wiki Ask 开关，POST 提交测试使用。
func enableMachineWikiAsk(srv *Server) {
	srv.env.WikiAskAPIEnabled = true
	srv.env.WikiAskAPIRPM = 60
}

func TestWikiAskMachineRequiresAPIKeyConfig(t *testing.T) {
	srv := testServer(t)
	res := performMachineRequest(t, srv, http.MethodPost, adminWikiAPIBasePath+"/ask:api", "test-key", `{"message":"hello"}`)
	if res.Code != http.StatusServiceUnavailable || !strings.Contains(res.Body.String(), `"error":"api key not configured"`) {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
}

func TestWikiAskMachineRejectsInvalidAPIKey(t *testing.T) {
	srv := testServer(t)
	srv.env.AigoniAPIKey = "test-key"
	res := performMachineRequest(t, srv, http.MethodPost, adminWikiAPIBasePath+"/ask:api", "wrong-key", `{"message":"hello"}`)
	if res.Code != http.StatusUnauthorized || !strings.Contains(res.Body.String(), `"error":"invalid api key"`) {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
}

func TestWikiAskMachineDisabledByDefault(t *testing.T) {
	srv := testServer(t)
	srv.env.AigoniAPIKey = "test-key"
	res := performMachineRequest(t, srv, http.MethodPost, adminWikiAPIBasePath+"/ask:api", "test-key", `{"message":"hello"}`)
	if res.Code != http.StatusForbidden || !strings.Contains(res.Body.String(), `"error":"wiki ask api disabled"`) {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
}

func TestWikiAskMachineRequiresMessage(t *testing.T) {
	srv := testServer(t)
	srv.env.AigoniAPIKey = "test-key"
	enableMachineWikiAsk(srv)
	for _, body := range []string{`{"message":"   "}`, `{}`} {
		res := performMachineRequest(t, srv, http.MethodPost, adminWikiAPIBasePath+"/ask:api", "test-key", body)
		if res.Code != http.StatusBadRequest || !strings.Contains(res.Body.String(), `"error":"message is required"`) {
			t.Fatalf("body %q status = %d, body = %s", body, res.Code, res.Body.String())
		}
	}
}

func TestWikiAskMachineRejectsUnknownFields(t *testing.T) {
	srv := testServer(t)
	srv.env.AigoniAPIKey = "test-key"
	enableMachineWikiAsk(srv)
	res := performMachineRequest(t, srv, http.MethodPost, adminWikiAPIBasePath+"/ask:api", "test-key", `{"message":"hello","title":"x"}`)
	if res.Code != http.StatusBadRequest || !strings.Contains(res.Body.String(), `"error":"invalid json body"`) {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
}

func TestWikiAskMachineInvalidJSON(t *testing.T) {
	srv := testServer(t)
	srv.env.AigoniAPIKey = "test-key"
	enableMachineWikiAsk(srv)
	for _, body := range []string{`{not json`, `{"message":"hello","history":`, `{"message":"hello"}trailing`} {
		res := performMachineRequest(t, srv, http.MethodPost, adminWikiAPIBasePath+"/ask:api", "test-key", body)
		if res.Code != http.StatusBadRequest || !strings.Contains(res.Body.String(), `"error":"invalid json body"`) {
			t.Fatalf("body %q status = %d, body = %s", body, res.Code, res.Body.String())
		}
	}
}

func TestWikiAskMachineWikiUnavailable(t *testing.T) {
	srv := testServer(t)
	srv.env.AigoniAPIKey = "test-key"
	enableMachineWikiAsk(srv)
	res := performMachineRequest(t, srv, http.MethodPost, adminWikiAPIBasePath+"/ask:api", "test-key", `{"message":"hello"}`)
	if res.Code != http.StatusServiceUnavailable || !strings.Contains(res.Body.String(), `"error":"wiki unavailable"`) {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
}

func TestWikiAskMachineRateLimited(t *testing.T) {
	srv := testServer(t)
	srv.env.AigoniAPIKey = "test-key"
	srv.env.WikiAskAPIEnabled = true
	srv.env.WikiAskAPIRPM = 1
	configureFakeWikiAgent(srv)

	first := performMachineRequest(t, srv, http.MethodPost, adminWikiAPIBasePath+"/ask:api", "test-key", `{"message":"hello"}`)
	if first.Code != http.StatusAccepted {
		t.Fatalf("first status = %d, body = %s", first.Code, first.Body.String())
	}
	second := performMachineRequest(t, srv, http.MethodPost, adminWikiAPIBasePath+"/ask:api", "test-key", `{"message":"hello"}`)
	if second.Code != http.StatusTooManyRequests || !strings.Contains(second.Body.String(), `"error":"rate limit exceeded"`) {
		t.Fatalf("second status = %d, body = %s", second.Code, second.Body.String())
	}
}

func TestWikiAskMachineAllowsConcurrentAskRuns(t *testing.T) {
	srv := testServer(t)
	srv.env.AigoniAPIKey = "test-key"
	enableMachineWikiAsk(srv)
	fake := configureBlockingWikiAgent(srv)

	// 并发未满时，多个 ask run 可以同时存在。
	first := performMachineRequest(t, srv, http.MethodPost, adminWikiAPIBasePath+"/ask:api", "test-key", `{"message":"first"}`)
	if first.Code != http.StatusAccepted {
		t.Fatalf("first status = %d, body = %s", first.Code, first.Body.String())
	}
	second := performMachineRequest(t, srv, http.MethodPost, adminWikiAPIBasePath+"/ask:api", "test-key", `{"message":"second"}`)
	if second.Code != http.StatusAccepted {
		t.Fatalf("second status = %d, body = %s", second.Code, second.Body.String())
	}
	close(fake.release)
}

func TestWikiAskMachineEnforcesConcurrencyLimit(t *testing.T) {
	srv := testServer(t)
	srv.env.AigoniAPIKey = "test-key"
	enableMachineWikiAsk(srv)
	srv.env.WikiAgentConcurrency = 1
	fake := configureBlockingWikiAgent(srv)

	first := performMachineRequest(t, srv, http.MethodPost, adminWikiAPIBasePath+"/ask:api", "test-key", `{"message":"first"}`)
	if first.Code != http.StatusAccepted {
		t.Fatalf("first status = %d, body = %s", first.Code, first.Body.String())
	}
	<-fake.started
	second := performMachineRequest(t, srv, http.MethodPost, adminWikiAPIBasePath+"/ask:api", "test-key", `{"message":"second"}`)
	if second.Code != http.StatusConflict || !strings.Contains(second.Body.String(), `"error":"wiki agent concurrency limit reached"`) {
		t.Fatalf("second status = %d, body = %s", second.Code, second.Body.String())
	}
	close(fake.release)
}

func TestWikiAskMachineRunPollDoesNotConsumeSubmitRPM(t *testing.T) {
	srv := testServer(t)
	srv.env.AigoniAPIKey = "test-key"
	srv.env.WikiAskAPIEnabled = true
	srv.env.WikiAskAPIRPM = 1
	configureFakeWikiAgent(srv)

	res := performMachineRequest(t, srv, http.MethodPost, adminWikiAPIBasePath+"/ask:api", "test-key", `{"message":"hello"}`)
	if res.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var run wikiAgentRun
	if err := json.NewDecoder(res.Body).Decode(&run); err != nil {
		t.Fatal(err)
	}
	// GET 轮询不消耗提交 RPM，即使提交上限为 1 也应可访问。
	poll := performMachineRequest(t, srv, http.MethodGet, adminWikiAPIBasePath+"/ask/runs/"+run.ID, "test-key", "")
	if poll.Code != http.StatusOK {
		t.Fatalf("poll status = %d, body = %s", poll.Code, poll.Body.String())
	}
}

func TestWikiAskMachineStartsReadOnlyRun(t *testing.T) {
	srv := testServer(t)
	srv.env.AigoniAPIKey = "test-key"
	enableMachineWikiAsk(srv)
	fake := configureFakeWikiAgent(srv)

	res := performMachineRequest(t, srv, http.MethodPost, adminWikiAPIBasePath+"/ask:api", "test-key", `{"message":"检查 Wiki","history":[{"role":"user","content":"上一轮问题"}]}`)
	if res.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	if loc := res.Header().Get("Location"); !strings.HasPrefix(loc, adminWikiAPIBasePath+"/ask/runs/") {
		t.Fatalf("location = %q", loc)
	}
	var run wikiAgentRun
	if err := json.NewDecoder(res.Body).Decode(&run); err != nil {
		t.Fatal(err)
	}
	if run.Kind != "ask" || len(run.ID) != 32 || run.Status != "pending" {
		t.Fatalf("run = %+v", run)
	}

	select {
	case req := <-fake.requests:
		if req.Message != "检查 Wiki" || len(req.History) != 1 || req.History[0].Content != "上一轮问题" {
			t.Fatalf("agent request = %+v", req)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("agent request was not sent")
	}

	waitWikiAgentRunStatus(t, srv, run.ID, "done")
	got := performMachineRequest(t, srv, http.MethodGet, adminWikiAPIBasePath+"/ask/runs/"+run.ID, "test-key", "")
	if got.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", got.Code, got.Body.String())
	}
	// PRD: run lookup returns status, answer, referenced files, and the event buffer.
	for _, want := range []string{`"status":"completed"`, `"answer"`, `"sources"`, `"files"`, `"events"`} {
		if !strings.Contains(got.Body.String(), want) {
			t.Fatalf("run response missing %s: %s", want, got.Body.String())
		}
	}
}

func TestWikiAskMachineBearerAuth(t *testing.T) {
	srv := testServer(t)
	srv.env.AigoniAPIKey = "test-key"
	enableMachineWikiAsk(srv)
	configureFakeWikiAgent(srv)

	req := httptest.NewRequest(http.MethodPost, adminWikiAPIBasePath+"/ask:api", strings.NewReader(`{"message":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-key")
	res := httptest.NewRecorder()
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var run wikiAgentRun
	if err := json.NewDecoder(res.Body).Decode(&run); err != nil {
		t.Fatal(err)
	}
	if run.Kind != "ask" {
		t.Fatalf("run kind = %q", run.Kind)
	}
}

func TestWikiAskMachineRunNotFound(t *testing.T) {
	srv := testServer(t)
	srv.env.AigoniAPIKey = "test-key"
	res := performMachineRequest(t, srv, http.MethodGet, adminWikiAPIBasePath+"/ask/runs/missing", "test-key", "")
	if res.Code != http.StatusNotFound || !strings.Contains(res.Body.String(), `"error":"wiki ask run not found"`) {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
}

func TestWikiAskMachineDoesNotRevealChatRun(t *testing.T) {
	srv := testServer(t)
	srv.env.AigoniAPIKey = "test-key"
	configureFakeWikiAgent(srv)
	chat, err := srv.startWikiAgentRunWithKind("chat", "hello", nil)
	if err != nil {
		t.Fatal(err)
	}
	res := performMachineRequest(t, srv, http.MethodGet, adminWikiAPIBasePath+"/ask/runs/"+chat.ID, "test-key", "")
	if res.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
}

func TestWikiAskMachineDoesNotWriteWikiFiles(t *testing.T) {
	srv := testServer(t)
	srv.env.AigoniAPIKey = "test-key"
	enableMachineWikiAsk(srv)
	configureFakeWikiAgent(srv)
	wikiRoot := filepath.Join(srv.root, "wiki")
	writeFile(t, filepath.Join(wikiRoot, "index.md"), "# Index\n")
	before := snapshotDirContents(t, wikiRoot)

	res := performMachineRequest(t, srv, http.MethodPost, adminWikiAPIBasePath+"/ask:api", "test-key", `{"message":"尝试修改 Wiki 文件"}`)
	if res.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var run wikiAgentRun
	if err := json.NewDecoder(res.Body).Decode(&run); err != nil {
		t.Fatal(err)
	}
	waitWikiAgentRunStatus(t, srv, run.ID, "done")

	after := snapshotDirContents(t, wikiRoot)
	if len(before) != len(after) {
		t.Fatalf("wiki dir changed: before = %v, after = %v", before, after)
	}
	for path, content := range before {
		if after[path] != content {
			t.Fatalf("wiki file %q changed: before = %q, after = %q", path, content, after[path])
		}
	}
}

func snapshotDirContents(t *testing.T, root string) map[string]string {
	t.Helper()
	snap := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		snap[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snap
}
