package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

type ctxKey string

const (
	reqIDKey          ctxKey = "req_id"
	conversationIDKey ctxKey = "conversation_id"
)

func GetReqID(ctx context.Context) string {
	if id, ok := ctx.Value(reqIDKey).(string); ok {
		return id
	}
	return ""
}

func GetConversationID(ctx context.Context) *string {
	if id, ok := ctx.Value(conversationIDKey).(string); ok && id != "" {
		return &id
	}
	return nil
}

// ConversationID extracts the conversationID from the JSON request body and
// stores it in the context so respond helpers can echo it back.
func ConversationID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body == nil || r.ContentLength == 0 {
			next.ServeHTTP(w, r)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))

		var partial struct {
			ConversationID *string `json:"conversationID"`
		}
		if err := json.Unmarshal(body, &partial); err == nil && partial.ConversationID != nil {
			ctx := context.WithValue(r.Context(), conversationIDKey, *partial.ConversationID)
			r = r.WithContext(ctx)
		}

		next.ServeHTTP(w, r)
	})
}

func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = fmt.Sprintf("req_%d", time.Now().UnixNano())
		}
		ctx := context.WithValue(r.Context(), reqIDKey, id)
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func Logger(log *zap.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := &responseWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(ww, r)
			log.Info("request",
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.Int("status", ww.status),
				zap.Duration("duration", time.Since(start)),
				zap.String("req_id", GetReqID(r.Context())),
			)
		})
	}
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// Tracing starts an OTel server span for every request and propagates the
// trace context. Must be added to the chi router (not wrapping it externally)
// so chi.RouteContext is available for low-cardinality span names.
func Tracing(next http.Handler) http.Handler {
	tracer := otel.Tracer("zia/api")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route := r.URL.Path
		if rctx := chi.RouteContext(r.Context()); rctx != nil && rctx.RoutePattern() != "" {
			route = rctx.RoutePattern()
		}
		ctx, span := tracer.Start(r.Context(), r.Method+" "+route,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				attribute.String("http.method", r.Method),
				attribute.String("http.route", route),
				attribute.String("http.url", r.URL.String()),
			),
		)
		defer span.End()

		ww := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(ww, r.WithContext(ctx))

		span.SetAttributes(attribute.Int("http.status_code", ww.status))
	})
}

func Recoverer(log *zap.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					log.Error("panic recovered",
						zap.Any("panic", rec),
						zap.String("req_id", GetReqID(r.Context())),
					)
					http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
