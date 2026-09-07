package utility

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
)

const (
	errWriteResponse  = "[RESPONSE_WRITE_FAILED] err=%v"
	headerContentType = "Content-Type"
	contentTypeJSON   = "application/json"
)

func WriteJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set(headerContentType, contentTypeJSON)
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	}); err != nil {
		log.Printf(errWriteResponse, err)
	}
}

func WriteJSONWithData(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set(headerContentType, contentTypeJSON)
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf(errWriteResponse, err)
	}
}

func GetRequestID(ctx context.Context) string {
	if v := ctx.Value("request_id"); v != nil {
		return v.(string)
	}
	return "unknown"
}
