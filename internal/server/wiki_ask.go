package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

func sanitizeWikiChatHistory(history []wikiChatMessage) []wikiChatMessage {
	const maxHistoryMessages = 20
	const maxMessageRunes = 4000
	if len(history) > maxHistoryMessages {
		history = history[len(history)-maxHistoryMessages:]
	}
	out := make([]wikiChatMessage, 0, len(history))
	for _, message := range history {
		role := strings.TrimSpace(strings.ToLower(message.Role))
		if role != "user" && role != "assistant" {
			continue
		}
		content := strings.TrimSpace(message.Content)
		if content == "" {
			continue
		}
		runes := []rune(content)
		if len(runes) > maxMessageRunes {
			content = string(runes[len(runes)-maxMessageRunes:])
		}
		out = append(out, wikiChatMessage{Role: role, Content: content})
	}
	return out
}

func (s *Server) writeWikiSSE(w http.ResponseWriter, flusher http.Flusher, event string, payload any) error {
	if w == nil || flusher == nil {
		return errors.New("wiki sse writer is not available")
	}
	data, err := json.Marshal(payload)
	if err != nil {
		data = []byte(`{"message":"failed to encode sse payload"}`)
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
	flusher.Flush()
	return err
}
