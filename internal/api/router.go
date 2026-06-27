package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

type Dependencies struct {
	Logger *zap.Logger
}

func NewRouter(deps Dependencies) http.Handler {
	r := chi.NewRouter()

	r.Use(RequestID)
	r.Use(Logger(deps.Logger))
	r.Use(Recoverer(deps.Logger))

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		respond(w, r, http.StatusOK, map[string]string{"status": "ok"})
	})

	return r
}
