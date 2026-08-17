package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// ChatModelRunner runs one Eino ChatModelAgent. Run is serialized because Eino
// does not document adk.Runner as safe for concurrent reuse.
type ChatModelRunner struct {
	mu     sync.Mutex
	runner *adk.Runner
}

// New creates a ChatModelAgent runner with prompt.md loaded as Instruction.
func New(ctx context.Context, cfg Config) (*ChatModelRunner, error) {
	if strings.TrimSpace(cfg.APIKey) == "" || strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.Model) == "" {
		return nil, errors.New("wiki agent model env is not configured")
	}
	instruction, err := LoadPromptFromRoot(cfg.Root)
	if err != nil {
		return nil, err
	}
	tools, err := NewTools(cfg)
	if err != nil {
		return nil, err
	}
	temperature := float32(0.2)
	model, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		APIKey:      cfg.APIKey,
		BaseURL:     strings.TrimRight(cfg.BaseURL, "/"),
		Model:       cfg.Model,
		Temperature: &temperature,
		Timeout:     10 * time.Minute,
	})
	if err != nil {
		return nil, err
	}
	maxIterations := cfg.MaxIterations
	if maxIterations <= 0 {
		maxIterations = 30
	}
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:          "aigoni_wiki_agent",
		Description:   "Maintains and answers questions from the local Aigoni Karpathy-style Wiki.",
		Instruction:   instruction,
		Model:         model,
		GenModelInput: genModelInputNoTemplate,
		MaxIterations: maxIterations,
		ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{
			Tools:               tools,
			ToolCallMiddlewares: []compose.ToolMiddleware{newToolEventMiddleware()},
		}},
	})
	if err != nil {
		return nil, err
	}
	return &ChatModelRunner{runner: adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent, EnableStreaming: true})}, nil
}

// Run executes one natural-language task with optional browser-provided history.
func (r *ChatModelRunner) Run(ctx context.Context, req RunRequest, sink EventSink) (*RunResult, error) {
	if r == nil || r.runner == nil {
		return nil, errors.New("wiki agent runner is nil")
	}
	message := strings.TrimSpace(req.Message)
	if message == "" {
		return nil, errors.New("message is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if sink != nil {
		sink.Emit(Event{Type: "step", Title: "开始 Wiki Agent", Status: "running"})
	}
	files := map[string]bool{}
	runCtx := context.WithValue(ctx, toolObserverContextKey{}, &toolEventObserver{sink: sink, files: files})
	iter := r.runner.Run(runCtx, buildMessages(req))
	var answer, reasoning strings.Builder
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event == nil {
			continue
		}
		if isWillRetryError(event.Err) {
			continue
		}
		if event.Err != nil {
			return nil, event.Err
		}
		if err := handleAgentEvent(event, sink, &answer, &reasoning); err != nil {
			return nil, err
		}
	}
	result := &RunResult{Answer: strings.TrimSpace(answer.String()), Reasoning: strings.TrimSpace(reasoning.String()), Files: sortedMapKeys(files)}
	result.Sources = extractSources(result.Answer)
	if sink != nil {
		sink.Emit(Event{Type: "step", Title: "Wiki Agent 完成", Status: "done"})
	}
	return result, nil
}

func genModelInputNoTemplate(_ context.Context, instruction string, input *adk.AgentInput) ([]*schema.Message, error) {
	inputLen := 0
	if input != nil {
		inputLen = len(input.Messages)
	}
	messages := make([]*schema.Message, 0, inputLen+1)
	if instruction != "" {
		messages = append(messages, schema.SystemMessage(instruction))
	}
	if input != nil {
		messages = append(messages, input.Messages...)
	}
	return messages, nil
}

func buildMessages(req RunRequest) []*schema.Message {
	messages := make([]*schema.Message, 0, len(req.History)+1)
	for _, item := range req.History {
		content := strings.TrimSpace(item.Content)
		if content == "" {
			continue
		}
		switch strings.TrimSpace(item.Role) {
		case "assistant":
			messages = append(messages, schema.AssistantMessage(content, nil))
		case "user":
			messages = append(messages, schema.UserMessage(content))
		}
	}
	messages = append(messages, schema.UserMessage(strings.TrimSpace(req.Message)))
	return messages
}

func handleAgentEvent(event *adk.AgentEvent, sink EventSink, answer, reasoning *strings.Builder) error {
	if event.Output == nil || event.Output.MessageOutput == nil {
		return nil
	}
	output := event.Output.MessageOutput
	if output.IsStreaming {
		return handleMessageStream(output, sink, answer, reasoning)
	}
	msg := output.Message
	if msg == nil {
		return nil
	}
	switch output.Role {
	case schema.Tool:
		return nil
	case schema.Assistant:
		if msg.Content != "" {
			answer.WriteString(msg.Content)
			if sink != nil {
				sink.Emit(Event{Type: "delta", Delta: msg.Content, Answer: answer.String()})
			}
		}
		if msg.ReasoningContent != "" {
			reasoning.WriteString(msg.ReasoningContent)
		}
	}
	return nil
}

func handleMessageStream(output *adk.MessageVariant, sink EventSink, answer, reasoning *strings.Builder) error {
	stream := output.MessageStream
	if stream == nil {
		return nil
	}
	defer stream.Close()
	if output.Role == schema.Tool {
		return drainToolMessageStream(stream)
	}
	var content, thinking strings.Builder
	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if isWillRetryError(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if msg == nil {
			continue
		}
		if msg.Content != "" {
			content.WriteString(msg.Content)
		}
		if msg.ReasoningContent != "" {
			thinking.WriteString(msg.ReasoningContent)
		}
	}
	if thinking.Len() > 0 {
		reasoning.WriteString(thinking.String())
	}
	if content.Len() > 0 {
		delta := content.String()
		answer.WriteString(delta)
		if sink != nil {
			sink.Emit(Event{Type: "delta", Delta: delta, Answer: answer.String()})
		}
	}
	return nil
}

func drainToolMessageStream(stream interface {
	Recv() (*schema.Message, error)
}) error {
	for {
		_, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if isWillRetryError(err) {
			return nil
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func isWillRetryError(err error) bool {
	if err == nil {
		return false
	}
	var retryErr *adk.WillRetryError
	return errors.As(err, &retryErr)
}

func sortedMapKeys(values map[string]bool) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func extractSources(markdown string) []string {
	matches := regexp.MustCompile(`(?:wiki|content/notes)/[^\s\)\]`+"`"+`]+?\.md`).FindAllString(markdown, -1)
	seen := map[string]bool{}
	var sources []string
	for _, match := range matches {
		path := strings.Trim(match, "`.,;，。；：")
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		sources = append(sources, path)
	}
	sort.Strings(sources)
	return sources
}

func (r *ChatModelRunner) String() string {
	return fmt.Sprintf("%T", r)
}
