// Package interfaces provides HTTP handlers for the OpsAgent API.
package interfaces

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Havens-blog/OpsAgent/internal/chat"
	"github.com/Havens-blog/OpsAgent/internal/guardrails"
	"github.com/Havens-blog/OpsAgent/internal/observability"
)

type createConversationRequest struct {
	Title string `json:"title"`
}

type renameConversationRequest struct {
	Title string `json:"title"`
}

type sendMessageRequest struct {
	Content         string `json:"content"`
	ClientMessageID string `json:"client_message_id,omitempty"`
}

// sseEvent is the JSON payload carried on each SSE "data:" line.
type sseEvent struct {
	Type           string                 `json:"type"`
	Content        string                 `json:"content,omitempty"`
	Message        string                 `json:"message,omitempty"`
	MessageID      string                 `json:"message_id,omitempty"`
	ConversationID string                 `json:"conversation_id,omitempty"`
	CallID         string                 `json:"call_id,omitempty"`
	ToolName       string                 `json:"tool_name,omitempty"`
	Params         map[string]interface{} `json:"params,omitempty"`
	Status         string                 `json:"status,omitempty"`
	Result         string                 `json:"result,omitempty"`
	Error          string                 `json:"error,omitempty"`
	ApprovalID     string                 `json:"approval_id,omitempty"`
	Reason         string                 `json:"reason,omitempty"`
}

// writeSSE writes one SSE event and flushes when the writer supports it.
func (s *APIServer) writeSSE(w http.ResponseWriter, ev sseEvent) error {
	b, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
		return err
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}

// conversationsHandler dispatches the collection endpoints.
func (s *APIServer) conversationsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listConversationsHandler(w, r)
	case http.MethodPost:
		s.createConversationHandler(w, r)
	default:
		s.writeErrorResponse(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed", "")
	}
}

// conversationHandler dispatches the single-conversation endpoints.
func (s *APIServer) conversationHandler(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/conversations/"), "/")
	parts := strings.Split(rest, "/")

	if len(parts) == 1 && parts[0] != "" {
		switch r.Method {
		case http.MethodGet:
			s.getConversationHandler(w, r, parts[0])
		case http.MethodDelete:
			s.deleteConversationHandler(w, r, parts[0])
		case http.MethodPatch:
			s.renameConversationHandler(w, r, parts[0])
		default:
			s.writeErrorResponse(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed", "")
		}
		return
	}

	if len(parts) == 2 && parts[1] == "messages" && parts[0] != "" {
		if r.Method != http.MethodPost {
			s.writeErrorResponse(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed", "")
			return
		}
		s.sendMessageHandler(w, r, parts[0])
		return
	}

	s.writeErrorResponse(w, http.StatusNotFound, "not_found", "Conversation endpoint not found", r.URL.Path)
}

// createConversationHandler handles POST /api/v1/conversations.
func (s *APIServer) createConversationHandler(w http.ResponseWriter, r *http.Request) {
	var req createConversationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeErrorResponse(w, http.StatusBadRequest, "invalid_request", "Failed to decode request", err.Error())
		return
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "新对话"
	}

	now := time.Now()
	conv := &chat.Conversation{
		ID:        uuid.New().String(),
		TenantID:  EIAMTenantID(r.Context()),
		UserID:    EIAMUID(r.Context()),
		Title:     title,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.conversationStore.Create(r.Context(), conv); err != nil {
		s.writeErrorResponse(w, http.StatusInternalServerError, "store_error", "Failed to create conversation", err.Error())
		return
	}

	s.writeJSONResponse(w, http.StatusCreated, map[string]interface{}{
		"conversation": conv,
	})
}

// listConversationsHandler handles GET /api/v1/conversations.
func (s *APIServer) listConversationsHandler(w http.ResponseWriter, r *http.Request) {
	convs, err := s.conversationStore.List(r.Context())
	if err != nil {
		s.writeErrorResponse(w, http.StatusInternalServerError, "store_error", "Failed to list conversations", err.Error())
		return
	}

	s.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
		"conversations": convs,
		"count":         len(convs),
	})
}

// getConversationHandler handles GET /api/v1/conversations/{id}.
func (s *APIServer) getConversationHandler(w http.ResponseWriter, r *http.Request, id string) {
	conv, err := s.conversationStore.Get(r.Context(), id)
	if err != nil {
		s.writeErrorResponse(w, http.StatusNotFound, "not_found", "Conversation not found", err.Error())
		return
	}

	messages, err := s.conversationStore.ListMessages(r.Context(), id)
	if err != nil {
		s.writeErrorResponse(w, http.StatusNotFound, "not_found", "Conversation messages not found", err.Error())
		return
	}

	pending := s.pendingApprovals.ListPendingForConversation(EIAMTenantID(r.Context()), EIAMUID(r.Context()), id)

	s.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
		"conversation":      conv,
		"messages":          messages,
		"pending_approvals": pending,
	})
}

// deleteConversationHandler handles DELETE /api/v1/conversations/{id}.
func (s *APIServer) deleteConversationHandler(w http.ResponseWriter, r *http.Request, id string) {
	if err := s.conversationStore.Delete(r.Context(), id); err != nil {
		s.writeErrorResponse(w, http.StatusNotFound, "not_found", "Conversation not found", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// renameConversationHandler handles PATCH /api/v1/conversations/{id}.
func (s *APIServer) renameConversationHandler(w http.ResponseWriter, r *http.Request, id string) {
	var req renameConversationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeErrorResponse(w, http.StatusBadRequest, "invalid_request", "Failed to decode request", err.Error())
		return
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		s.writeErrorResponse(w, http.StatusBadRequest, "invalid_request", "Title is required", "")
		return
	}

	if err := s.conversationStore.UpdateTitle(r.Context(), id, title); err != nil {
		s.writeErrorResponse(w, http.StatusNotFound, "not_found", "Conversation not found", err.Error())
		return
	}

	conv, err := s.conversationStore.Get(r.Context(), id)
	if err != nil {
		s.writeErrorResponse(w, http.StatusInternalServerError, "store_error", "Failed to load conversation", err.Error())
		return
	}

	s.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
		"conversation": conv,
	})
}

// sendMessageHandler handles POST /api/v1/conversations/{id}/messages as SSE.
func (s *APIServer) sendMessageHandler(w http.ResponseWriter, r *http.Request, id string) {
	var req sendMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeErrorResponse(w, http.StatusBadRequest, "invalid_request", "Failed to decode request", err.Error())
		return
	}

	content := strings.TrimSpace(req.Content)
	if content == "" {
		s.writeErrorResponse(w, http.StatusBadRequest, "invalid_request", "Content is required", "")
		return
	}

	// Conversation must exist and belong to the ctx tenant.
	if _, err := s.conversationStore.Get(r.Context(), id); err != nil {
		s.writeErrorResponse(w, http.StatusNotFound, "not_found", "Conversation not found", err.Error())
		return
	}

	// Snapshot the chat service before streaming.
	chatSvc := s.getChatService()
	if chatSvc == nil || !chatSvc.IsAvailable() {
		s.writeErrorResponse(w, http.StatusServiceUnavailable, "service_unavailable",
			"Chat service not available. Please configure LLM_API_KEY in config/settings.yaml or environment variables.", "")
		return
	}

	history, err := s.conversationStore.ListMessages(r.Context(), id)
	if err != nil {
		s.writeErrorResponse(w, http.StatusInternalServerError, "store_error", "Failed to load history", err.Error())
		return
	}

	alreadyPersisted := false
	for _, m := range history {
		if req.ClientMessageID != "" && m.Role == "user" && m.ClientMessageID == req.ClientMessageID {
			alreadyPersisted = true
			break
		}
	}

	// A retry after an approval carries the same client_message_id as the
	// original send; reuse the already-persisted user message instead of
	// appending a duplicate.
	if req.ClientMessageID == "" || !alreadyPersisted {
		userMsg := &chat.Message{
			ID:              uuid.New().String(),
			Role:            "user",
			Content:         content,
			ClientMessageID: req.ClientMessageID,
			CreatedAt:       time.Now(),
		}
		if err := s.conversationStore.AddMessage(r.Context(), id, userMsg); err != nil {
			s.writeErrorResponse(w, http.StatusInternalServerError, "store_error", "Failed to persist message", err.Error())
			return
		}
		history, err = s.conversationStore.ListMessages(r.Context(), id)
		if err != nil {
			s.writeErrorResponse(w, http.StatusInternalServerError, "store_error", "Failed to load history", err.Error())
			return
		}
	}

	messages := make([]chat.Message, 0, len(history))
	for _, m := range history {
		messages = append(messages, chat.Message{Role: m.Role, Content: m.Content})
	}

	// SSE headers are set before any streaming output is written.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ctx := withSessionID(r.Context(), id)
	// The approval callback's parameter shadows req; keep the client id here.
	clientMessageID := req.ClientMessageID

	handler := chat.StreamHandler{
		OnDelta: func(delta string) error {
			return s.writeSSE(w, sseEvent{Type: "delta", Content: delta})
		},
		OnToolCall: func(callID string, name string, params map[string]interface{}) error {
			return s.writeSSE(w, sseEvent{
				Type:     "tool_call",
				CallID:   callID,
				ToolName: name,
				Params:   params,
				Status:   "running",
			})
		},
		OnToolResult: func(callID string, name string, status string, result string, errMsg string) error {
			return s.writeSSE(w, sseEvent{
				Type:     "tool_result",
				CallID:   callID,
				ToolName: name,
				Status:   status,
				Result:   result,
				Error:    errMsg,
			})
		},
		OnApprovalRequired: func(req guardrails.ApprovalRequest) error {
			approvalID := uuid.New().String()
			expiry := req.ExpirySeconds
			if expiry <= 0 {
				expiry = 300
			}
			scope := req.Scope
			if scope == "" {
				scope = guardrails.ScopeOneShot
			}
			now := time.Now()

			pending := &PendingApproval{
				ApprovalID:      approvalID,
				ToolName:        req.ToolName,
				Params:          req.Params,
				Reason:          req.Reason,
				ConversationID:  id,
				TenantID:        EIAMTenantID(ctx),
				UserID:          EIAMUID(ctx),
				ParamHash:       guardrails.ParamHash(req.Params),
				Scope:           scope,
				ExpirySeconds:   expiry,
				ExpiresAt:       now.Add(time.Duration(expiry) * time.Second),
				Status:          "pending",
				CreatedAt:       now,
				ClientMessageID: clientMessageID,
			}
			if err := s.pendingApprovals.Create(pending); err != nil {
				return err
			}
			return s.writeSSE(w, sseEvent{
				Type:       "approval_required",
				ApprovalID: approvalID,
				ToolName:   req.ToolName,
				Params:     req.Params,
				Reason:     req.Reason,
			})
		},
	}

	fullText, err := chatSvc.StreamWithTools(ctx, messages, handler)

	if err != nil {
		// Client disconnection cancels the request; there is no peer left to
		// receive an error event, so just stop without persisting a reply.
		if ctx.Err() == nil {
			_ = s.writeSSE(w, sseEvent{Type: "error", Message: err.Error()})
		}
		return
	}

	assistantMsg := &chat.Message{
		ID:        uuid.New().String(),
		Role:      "assistant",
		Content:   fullText,
		CreatedAt: time.Now(),
	}
	if err := s.conversationStore.AddMessage(ctx, id, assistantMsg); err != nil {
		s.logger.Error("Failed to persist assistant message",
			observability.String("conversation_id", id),
			observability.Err(err),
		)
		_ = s.writeSSE(w, sseEvent{Type: "error", Message: "failed to persist assistant message"})
		return
	}

	_ = s.writeSSE(w, sseEvent{Type: "done", MessageID: assistantMsg.ID, ConversationID: id})
}
