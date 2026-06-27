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

func respond(w http.ResponseWriter, r *http.Request, status int, data any) {
	msgID := GetReqID(r.Context())
	resp := ResponseEnvelope{
		StatusCode:         "0",
		StatusDescription:  "Success",
		MessageCode:        strconv.Itoa(status),
		MessageDescription: http.StatusText(status),
		MessageID:          msgID,
		PrimaryData:        data,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		zap.L().Error("failed to encode response", zap.Error(err))
	}
}

func respondError(w http.ResponseWriter, r *http.Request, status int, code, msg string) {
	msgID := GetReqID(r.Context())
	resp := ResponseEnvelope{
		StatusCode:         code,
		StatusDescription:  "BusinessError",
		MessageCode:        strconv.Itoa(status),
		MessageDescription: msg,
		MessageID:          msgID,
		ErrorInfo: &ErrorInfo{
			ErrorCode:    code,
			ErrorMessage: msg,
		},
		PrimaryData: nil,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		zap.L().Error("failed to encode error response", zap.Error(err))
	}
}

func respondValidationError(w http.ResponseWriter, r *http.Request, fieldErrors []FieldError) {
	msgID := GetReqID(r.Context())
	resp := ResponseEnvelope{
		StatusCode:         "1001",
		StatusDescription:  "Validation error",
		MessageCode:        "400",
		MessageDescription: "One or more fields failed validation",
		MessageID:          msgID,
		ErrorInfo: &ErrorInfo{
			ErrorCode:    "1001",
			ErrorMessage: "One or more fields failed validation",
			FieldErrors:  fieldErrors,
		},
		PrimaryData: nil,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		zap.L().Error("failed to encode validation error response", zap.Error(err))
	}
}
