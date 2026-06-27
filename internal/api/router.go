package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

type Dependencies struct {
	Logger        *zap.Logger
	PIHandler     *PaymentIntentHandler
	WebhookHandler *WebhookHandler
}

func NewRouter(deps Dependencies) http.Handler {
	r := chi.NewRouter()

	r.Use(RequestID)
	r.Use(Logger(deps.Logger))
	r.Use(Recoverer(deps.Logger))

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		respond(w, r, http.StatusOK, map[string]string{"status": "ok"})
	})

	r.Post("/v1/webhooks/{psp}", deps.WebhookHandler.Ingest)

	r.Route("/v1/payment_intents", func(r chi.Router) {
		r.Post("/", deps.PIHandler.Create)
		r.Get("/{id}", deps.PIHandler.Get)
	})

	return r
}
