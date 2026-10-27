// Package interfaces provides HTTP handlers for the OpsAgent API.
package interfaces

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/chat"
	"github.com/Havens-blog/OpsAgent/internal/observability"
)

// ChatRequest represents a chat request from the frontend.
type ChatRequest struct {
	Messages []ChatMessage `json:"messages"`
}

// ChatMessage represents a single message in the conversation.
type ChatMessage struct {
	Role    string `json:"role"` // "user" or "assistant"
	Content string `json:"content"`
}

// ChatResponse represents the response from the chat service.
type ChatResponse struct {
	Reply      string    `json:"reply"`
	Agent      string    `json:"agent"` // "Coordinator" for MVP
	Timestamp  string    `json:"timestamp"`
	Model      string    `json:"model,omitempty"`
	Thinking   []ThinkingStep `json:"thinking,omitempty"`
	Tools      []ToolCall    `json:"tools,omitempty"`
}

// ThinkingStep represents a step in the agent's thinking process (optional, for future tool-calling).
type ThinkingStep struct {
	Step   string `json:"step"`
	Detail string `json:"detail"`
}

// ToolCall represents a tool execution (optional, for future tool-calling).
type ToolCall struct {
	Tool   string                 `json:"tool"`
	Params map[string]interface{} `json:"params"`
	Result string                 `json:"result"`
}

// chatHandler handles chat requests.
func (s *APIServer) chatHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeErrorResponse(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST is allowed", "")
		return
	}

	// Parse request
	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeErrorResponse(w, http.StatusBadRequest, "invalid_request", "Failed to decode request", err.Error())
		return
	}

	// Validate messages
	if len(req.Messages) == 0 {
		s.writeErrorResponse(w, http.StatusBadRequest, "invalid_request", "Messages array is empty", "")
		return
	}

	// Last message should be from user
	lastMsg := req.Messages[len(req.Messages)-1]
	if lastMsg.Role != "user" {
		s.writeErrorResponse(w, http.StatusBadRequest, "invalid_request", "Last message must be from user", "")
		return
	}

	// Snapshot the chat service under lock — the config endpoints can rebuild
	// it concurrently when the user saves a new model configuration.
	chatSvc := s.getChatService()

	// Check if chat service is available
	if chatSvc == nil || !chatSvc.IsAvailable() {
		s.writeErrorResponse(w, http.StatusServiceUnavailable, "service_unavailable",
			"Chat service not available. Please configure LLM_API_KEY in config/settings.yaml or environment variables.", "")
		return
	}

	s.logger.Info("Chat request received",
		observability.Int("message_count", len(req.Messages)),
		observability.String("model", chatSvc.GetModel()),
	)

	// Convert to chat service format
	messages := make([]chat.Message, len(req.Messages))
	for i, m := range req.Messages {
		messages[i] = chat.Message{
			Role:    m.Role,
			Content: m.Content,
		}
	}

	// Call chat service with timeout
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	reply, err := chatSvc.Chat(ctx, messages)
	if err != nil {
		s.logger.Error("Chat service call failed",
			observability.Err(err),
		)
		s.writeErrorResponse(w, http.StatusInternalServerError, "chat_error", "Failed to get response from LLM", err.Error())
		return
	}

	// Build response
	response := ChatResponse{
		Reply:     reply,
		Agent:     "Coordinator", // MVP: single agent
		Timestamp: time.Now().Format(time.RFC3339),
		Model:     chatSvc.GetModel(),
		// Thinking and Tools are empty for MVP (no tool-calling yet)
	}

	s.writeJSONResponse(w, http.StatusOK, response)
}
