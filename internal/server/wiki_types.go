package server

type wikiChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type wikiDoc struct {
	Path    string
	Content string
}

type wikiSSEPayload struct {
	Type       string `json:"type,omitempty"`
	Title      string `json:"title,omitempty"`
	Name       string `json:"name,omitempty"`
	Tool       string `json:"tool,omitempty"`
	Status     string `json:"status,omitempty"`
	Summary    string `json:"summary,omitempty"`
	Args       any    `json:"args,omitempty"`
	Result     any    `json:"result,omitempty"`
	StartedAt  string `json:"started_at,omitempty"`
	DurationMS *int64 `json:"duration_ms,omitempty"`
	Error      string `json:"error,omitempty"`
	Reasoning  string `json:"reasoning,omitempty"`
	Message    string `json:"message,omitempty"`
}
