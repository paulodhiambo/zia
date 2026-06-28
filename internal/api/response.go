package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"go.uber.org/zap"
)

type KeyValuePair struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type ErrorInfo struct {
	ErrorCode    string `json:"errorCode"`
	ErrorMessage string `json:"errorMessage"`
	FieldErrors  []FieldError `json:"fieldErrors,omitempty"`
}

type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type ResponseEnvelope struct {
	StatusCode         string            `json:"statusCode"`
	StatusDescription  string            `json:"statusDescription"`
	MessageCode        string            `json:"messageCode"`
	MessageDescription string            `json:"messageDescription"`
	ErrorInfo          *ErrorInfo        `json:"errorInfo"`
	MessageID          string            `json:"messageID"`
	ConversationID     *string           `json:"conversationID"`
	AdditionalData     []KeyValuePair    `json:"additionalData"`
	PrimaryData        any               `json:"primaryData"`
}

type RequestEnvelope struct {
	MessageID       string         `json:"messageID"`
	ConversationID  *string        `json:"conversationID"`
	PrimaryData     json.RawMessage `json:"primaryData"`
	AdditionalData  []KeyValuePair `json:"additionalData"`
}

func buildEnvelope(r *http.Request, statusCode, statusDesc, msgCode, msgDesc string, data any, errInfo *ErrorInfo) ResponseEnvelope {
	return ResponseEnvelope{
		StatusCode:         statusCode,
		StatusDescription:  statusDesc,
		MessageCode:        msgCode,
		MessageDescription: msgDesc,
		ErrorInfo:          errInfo,
		MessageID:          GetReqID(r.Context()),
		ConversationID:     GetConversationID(r.Context()),
		PrimaryData:        data,
	}
}

func respond(w http.ResponseWriter, r *http.Request, status int, data any) {
	resp := buildEnvelope(r, "0", "Success", strconv.Itoa(status), http.StatusText(status), data, nil)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		zap.L().Error("failed to encode response", zap.Error(err))
	}
}

func respondError(w http.ResponseWriter, r *http.Request, status int, code, msg string) {
	resp := buildEnvelope(r, code, "BusinessError", strconv.Itoa(status), msg, nil, &ErrorInfo{
		ErrorCode:    code,
		ErrorMessage: msg,
	})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		zap.L().Error("failed to encode error response", zap.Error(err))
	}
}

func respondValidationError(w http.ResponseWriter, r *http.Request, fieldErrors []FieldError) {
	resp := buildEnvelope(r, "1001", "Validation error", "400", "One or more fields failed validation", nil, &ErrorInfo{
		ErrorCode:    "1001",
		ErrorMessage: "One or more fields failed validation",
		FieldErrors:  fieldErrors,
	})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		zap.L().Error("failed to encode validation error response", zap.Error(err))
	}
}
