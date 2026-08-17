package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func TestBuildMessagesFiltersAndTrimsHistory(t *testing.T) {
	messages := buildMessages(RunRequest{Message: "  当前问题  ", History: []ChatMessage{
		{Role: "user", Content: "  上一轮问题  "},
		{Role: "assistant", Content: "上一轮回答"},
		{Role: "system", Content: "ignore"},
		{Role: "user", Content: "   "},
	}})
	if len(messages) != 3 {
		t.Fatalf("messages len = %d, want 3", len(messages))
	}
	if messages[0].Role != schema.User || messages[0].Content != "上一轮问题" {
		t.Fatalf("first message = %+v", messages[0])
	}
	if messages[1].Role != schema.Assistant || messages[1].Content != "上一轮回答" {
		t.Fatalf("second message = %+v", messages[1])
	}
	if messages[2].Role != schema.User || messages[2].Content != "当前问题" {
		t.Fatalf("last message = %+v", messages[2])
	}
}

func TestGenModelInputNoTemplateKeepsLiteralBraces(t *testing.T) {
	instruction := `输出 JSON 示例：{"path":"wiki/index.md"}`
	messages, err := genModelInputNoTemplate(context.Background(), instruction, &adk.AgentInput{Messages: []*schema.Message{schema.UserMessage("问")}})
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || messages[0].Content != instruction {
		t.Fatalf("messages = %+v", messages)
	}
}

func TestBuildToolEventSanitizesReadContent(t *testing.T) {
	event := buildToolEvent("read", `{"path":"wiki/index.md"}`, `{"path":"wiki/index.md","content":"secret markdown","truncated":true}`, time.Now(), nil)
	if event.Type != "tool" || event.Tool != "read" || event.Status != "done" {
		t.Fatalf("event = %+v", event)
	}
	args, ok := event.Args.(map[string]any)
	if !ok || args["path"] != "wiki/index.md" {
		t.Fatalf("args = %#v", event.Args)
	}
	result, ok := event.Result.(map[string]any)
	if !ok || result["bytes"] != len("secret markdown") || result["truncated"] != true {
		t.Fatalf("result = %#v", event.Result)
	}
}

func TestToolObserverEmitsStructuredWriteAndCollectsChangedFile(t *testing.T) {
	var sink captureSink
	files := map[string]bool{}
	observer := &toolEventObserver{sink: &sink, files: files}
	observer.emit("write", `{"path":"wiki/syntheses/test.md","content":"hidden"}`, `{"path":"wiki/syntheses/test.md","bytes":6,"changed":true}`, time.Now(), nil)
	if len(sink.events) != 1 {
		t.Fatalf("events = %+v", sink.events)
	}
	event := sink.events[0]
	if event.Tool != "write" || event.Status != "done" {
		t.Fatalf("event = %+v", event)
	}
	args, ok := event.Args.(map[string]any)
	if !ok || args["path"] != "wiki/syntheses/test.md" || args["content"] != nil {
		t.Fatalf("args = %#v", event.Args)
	}
	result, ok := event.Result.(map[string]any)
	if !ok || result["bytes"] != 6 || result["changed"] != true {
		t.Fatalf("result = %#v", event.Result)
	}
	if !files["wiki/syntheses/test.md"] {
		t.Fatalf("files = %+v", files)
	}
}

func TestHandleMessageStreamCommitsAssistantStreamAfterEOF(t *testing.T) {
	stream := schema.StreamReaderFromArray([]*schema.Message{
		{Content: "hel", ReasoningContent: "think-"},
		{Content: "lo", ReasoningContent: "done"},
	})
	output := &adk.MessageVariant{IsStreaming: true, MessageStream: stream, Role: schema.Assistant}
	var sink captureSink
	var answer, reasoning strings.Builder
	answer.WriteString("prefix ")
	if err := handleMessageStream(output, &sink, &answer, &reasoning); err != nil {
		t.Fatal(err)
	}
	if answer.String() != "prefix hello" || reasoning.String() != "think-done" {
		t.Fatalf("answer = %q reasoning = %q", answer.String(), reasoning.String())
	}
	if len(sink.events) != 1 || sink.events[0].Delta != "hello" || sink.events[0].Answer != "prefix hello" {
		t.Fatalf("events = %+v", sink.events)
	}
}

func TestHandleMessageStreamDiscardsWillRetryPartial(t *testing.T) {
	base := schema.StreamReaderFromArray([]*schema.Message{{Content: "partial", ReasoningContent: "thinking"}})
	stream := schema.StreamReaderWithConvert(base, func(msg *schema.Message) (*schema.Message, error) {
		return msg, nil
	}, schema.WithOnEOF(func() (any, error) {
		return nil, &adk.WillRetryError{ErrStr: "retry", RetryAttempt: 1}
	}))
	output := &adk.MessageVariant{IsStreaming: true, MessageStream: stream, Role: schema.Assistant}
	var sink captureSink
	var answer, reasoning strings.Builder
	if err := handleMessageStream(output, &sink, &answer, &reasoning); err != nil {
		t.Fatal(err)
	}
	if answer.String() != "" || reasoning.String() != "" || len(sink.events) != 0 {
		t.Fatalf("answer = %q reasoning = %q events = %+v", answer.String(), reasoning.String(), sink.events)
	}
}

func TestExtractSourcesDeduplicatesAndSorts(t *testing.T) {
	sources := extractSources("见 wiki/index.md 与 content/notes/2026/a.md，也见 wiki/index.md；另见 content/notes/2026/2026-08-08-2-你好，我是huo0.md。")
	want := []string{"content/notes/2026/2026-08-08-2-你好，我是huo0.md", "content/notes/2026/a.md", "wiki/index.md"}
	if len(sources) != len(want) {
		t.Fatalf("sources = %+v", sources)
	}
	for i := range want {
		if sources[i] != want[i] {
			t.Fatalf("sources = %+v, want %+v", sources, want)
		}
	}
}

type captureSink struct {
	events []Event
}

func (s *captureSink) Emit(event Event) {
	s.events = append(s.events, event)
}
