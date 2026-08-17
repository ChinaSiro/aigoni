package agent

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

type toolObserverContextKey struct{}

type toolEventObserver struct {
	sink  EventSink
	files map[string]bool
	mu    sync.Mutex
}

func newToolEventMiddleware() compose.ToolMiddleware {
	return compose.ToolMiddleware{
		Invokable: func(next compose.InvokableToolEndpoint) compose.InvokableToolEndpoint {
			return func(ctx context.Context, input *compose.ToolInput) (*compose.ToolOutput, error) {
				startedAt := time.Now()
				output, err := next(ctx, input)
				result := ""
				if output != nil {
					result = output.Result
				}
				emitObservedToolEvent(ctx, input.Name, input.Arguments, result, startedAt, err)
				return output, err
			}
		},
		Streamable: func(next compose.StreamableToolEndpoint) compose.StreamableToolEndpoint {
			return func(ctx context.Context, input *compose.ToolInput) (*compose.StreamToolOutput, error) {
				startedAt := time.Now()
				output, err := next(ctx, input)
				if err != nil || output == nil || output.Result == nil {
					emitObservedToolEvent(ctx, input.Name, input.Arguments, "", startedAt, err)
					return output, err
				}
				var result strings.Builder
				stream := schema.StreamReaderWithConvert(output.Result, func(chunk string) (string, error) {
					result.WriteString(chunk)
					return chunk, nil
				}, schema.WithOnEOF(func() (any, error) {
					emitObservedToolEvent(ctx, input.Name, input.Arguments, result.String(), startedAt, nil)
					return nil, io.EOF
				}), schema.WithErrWrapper(func(err error) error {
					emitObservedToolEvent(ctx, input.Name, input.Arguments, result.String(), startedAt, err)
					return err
				}))
				return &compose.StreamToolOutput{Result: stream}, nil
			}
		},
	}
}

func emitObservedToolEvent(ctx context.Context, toolName, argsJSON, output string, startedAt time.Time, err error) {
	observer, _ := ctx.Value(toolObserverContextKey{}).(*toolEventObserver)
	if observer == nil {
		return
	}
	observer.emit(toolName, argsJSON, output, startedAt, err)
}

func (o *toolEventObserver) emit(toolName, argsJSON, output string, startedAt time.Time, err error) {
	if o == nil {
		return
	}
	event := buildToolEvent(toolName, argsJSON, output, startedAt, err)
	if err == nil {
		o.collectWriteFile(toolName, output)
	}
	if o.sink != nil {
		o.sink.Emit(event)
	}
}

func (o *toolEventObserver) collectWriteFile(toolName, output string) {
	if o == nil || o.files == nil {
		return
	}
	path, changed := parseWriteFile(output)
	if toolName != "write" || path == "" || !changed {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.files[path] = true
}

func buildToolEvent(toolName, argsJSON, output string, startedAt time.Time, err error) Event {
	durationMS := time.Since(startedAt).Milliseconds()
	event := Event{
		Type:       "tool",
		Name:       toolName,
		Tool:       toolName,
		Status:     "done",
		Summary:    toolSummary(toolName),
		Message:    toolSummary(toolName),
		Args:       safeToolArgs(toolName, argsJSON),
		Result:     safeToolResult(toolName, output),
		StartedAt:  startedAt.Format(time.RFC3339),
		DurationMS: &durationMS,
	}
	if err != nil {
		event.Status = "error"
		event.Message = err.Error()
		event.Error = err.Error()
		event.Result = nil
	}
	return event
}

func toolSummary(toolName string) string {
	switch toolName {
	case "read":
		return "读取知识文件"
	case "grep":
		return "搜索相关内容"
	case "glob":
		return "查找文件"
	case "write":
		return "更新 Wiki"
	default:
		return "调用工具"
	}
}

func safeToolArgs(toolName, argsJSON string) map[string]any {
	var args map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return nil
	}
	out := map[string]any{}
	copyStringArg := func(name string) {
		if value, ok := args[name].(string); ok && value != "" {
			out[name] = value
		}
	}
	switch toolName {
	case "read":
		copyStringArg("path")
	case "grep":
		copyStringArg("query")
		copyStringArg("path")
	case "glob":
		copyStringArg("pattern")
		copyStringArg("path")
	case "write":
		copyStringArg("path")
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func safeToolResult(toolName, output string) map[string]any {
	switch toolName {
	case "glob":
		var parsed globOutput
		if err := json.Unmarshal([]byte(output), &parsed); err != nil {
			return nil
		}
		result := map[string]any{"count": len(parsed.Items)}
		if parsed.Truncated {
			result["truncated"] = true
		}
		return result
	case "grep":
		var parsed grepOutput
		if err := json.Unmarshal([]byte(output), &parsed); err != nil {
			return nil
		}
		files := map[string]bool{}
		for _, match := range parsed.Matches {
			if match.Path != "" {
				files[match.Path] = true
			}
		}
		result := map[string]any{"matches": len(parsed.Matches), "files": len(files)}
		if parsed.Truncated {
			result["truncated"] = true
		}
		return result
	case "read":
		var parsed readOutput
		if err := json.Unmarshal([]byte(output), &parsed); err != nil {
			return nil
		}
		result := map[string]any{"bytes": len(parsed.Content)}
		if parsed.Truncated {
			result["truncated"] = true
		}
		return result
	case "write":
		var parsed writeOutput
		if err := json.Unmarshal([]byte(output), &parsed); err != nil {
			return nil
		}
		result := map[string]any{"bytes": parsed.Bytes, "changed": parsed.Changed}
		if parsed.Truncated {
			result["truncated"] = true
		}
		return result
	default:
		return nil
	}
}

func parseWriteFile(output string) (string, bool) {
	var parsed struct {
		Path    string `json:"path"`
		Changed *bool  `json:"changed"`
	}
	if err := json.Unmarshal([]byte(output), &parsed); err != nil || parsed.Path == "" {
		return "", false
	}
	if parsed.Changed != nil && !*parsed.Changed {
		return parsed.Path, false
	}
	return parsed.Path, true
}
