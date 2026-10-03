package gateway

import (
	"encoding/json"
	"net/http"
)

// writeError writes an OpenAI-shaped error body.
func writeError(w http.ResponseWriter, status int, code, msg string) {
	typ := "invalid_request_error"
	if status >= 500 {
		typ = "api_error"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{"message": msg, "type": typ, "code": code},
	})
}
