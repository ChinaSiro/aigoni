package agent

import "context"

// Config describes the local workspace and model settings used by the Wiki Agent.
type Config struct {
	Root    string
	APIKey  string
	BaseURL string
	Model   string

	MaxIterations int
	Limits        ToolLimits
	WriteLock     Locker
	// ReadOnly omits the write tool so the agent can only glob/grep/read
	// wiki and content/notes files. It is the code-level boundary for
	// machine-facing read-only ask runs.
	ReadOnly bool
}

// Locker is the small subset of sync.Mutex used by write tools.
type Locker interface {
	Lock()
	Unlock()
}

// ToolLimits bounds tool output and write size so agent context stays controlled.
type ToolLimits struct {
	GlobMaxResults int
	GrepMaxFiles   int
	GrepMaxMatches int
	ReadMaxBytes   int64
	WriteMaxBytes  int64
}

// ChatMessage is a browser-owned history item passed back for the next run.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// RunRequest is one natural-language task for the Wiki Agent.
type RunRequest struct {
	Message string        `json:"message"`
	History []ChatMessage `json:"history,omitempty"`
}

// RunResult is the final agent output after one run.
type RunResult struct {
	Answer    string   `json:"answer"`
	Reasoning string   `json:"reasoning,omitempty"`
	Sources   []string `json:"sources,omitempty"`
	Files     []string `json:"files,omitempty"`
}

// Event reports observable agent progress to the server run manager.
type Event struct {
	Type       string `json:"type"`
	Title      string `json:"title,omitempty"`
	Name       string `json:"name,omitempty"`
	Tool       string `json:"tool,omitempty"`
	Status     string `json:"status,omitempty"`
	Summary    string `json:"summary,omitempty"`
	Message    string `json:"message,omitempty"`
	Delta      string `json:"delta,omitempty"`
	Answer     string `json:"answer,omitempty"`
	Reasoning  string `json:"reasoning,omitempty"`
	Args       any    `json:"args,omitempty"`
	Result     any    `json:"result,omitempty"`
	StartedAt  string `json:"started_at,omitempty"`
	DurationMS *int64 `json:"duration_ms,omitempty"`
	Error      string `json:"error,omitempty"`
}

// EventSink receives events emitted while a run is executing.
type EventSink interface {
	Emit(Event)
}

// EventSinkFunc adapts a function to EventSink.
type EventSinkFunc func(Event)

func (f EventSinkFunc) Emit(e Event) { f(e) }

// Runner executes one Wiki Agent task.
type Runner interface {
	Run(ctx context.Context, req RunRequest, sink EventSink) (*RunResult, error)
}
