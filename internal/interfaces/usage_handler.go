package interfaces

import "net/http"

type usageTenantDTO struct {
	TenantID         int64 `json:"tenant_id"`
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
	LLMCalls         int64 `json:"llm_calls"`
	ToolCalls        int64 `json:"tool_calls"`
}

type usageUserDTO struct {
	TenantID         int64 `json:"tenant_id"`
	UserID           int64 `json:"user_id"`
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
	LLMCalls         int64 `json:"llm_calls"`
	ToolCalls        int64 `json:"tool_calls"`
}

// usageHandler returns the current caller's tenant aggregate and their own
// user aggregate. It is observational only and never enforces a quota.
func (s *APIServer) usageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeErrorResponse(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET is allowed", "")
		return
	}

	tenantID := EIAMTenantID(r.Context())
	userID := EIAMUID(r.Context())

	tenantBucket := s.usage.tenantAggregate(tenantID)
	userBucket := s.usage.userAggregate(tenantID, userID)

	payload := map[string]interface{}{
		"tenant": usageTenantDTO{
			TenantID:         tenantID,
			PromptTokens:     tenantBucket.PromptTokens,
			CompletionTokens: tenantBucket.CompletionTokens,
			TotalTokens:      tenantBucket.TotalTokens,
			LLMCalls:         tenantBucket.LLMCalls,
			ToolCalls:        tenantBucket.ToolCalls,
		},
		"user": usageUserDTO{
			TenantID:         tenantID,
			UserID:           userID,
			PromptTokens:     userBucket.PromptTokens,
			CompletionTokens: userBucket.CompletionTokens,
			TotalTokens:      userBucket.TotalTokens,
			LLMCalls:         userBucket.LLMCalls,
			ToolCalls:        userBucket.ToolCalls,
		},
	}

	s.writeJSONResponse(w, http.StatusOK, payload)
}
