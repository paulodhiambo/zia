package api

import (
	"embed"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

//go:embed checkout.js
var checkoutFS embed.FS

type Dependencies struct {
	Logger           *zap.Logger
	PIHandler        *PaymentIntentHandler
	WebhookHandler   *WebhookHandler
	MerchantHandler  *MerchantHandler
	CheckoutHandler  *CheckoutHandler
	AuthMiddleware   func(http.Handler) http.Handler
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
		r.With(deps.AuthMiddleware).Post("/", deps.PIHandler.Create)
		r.With(deps.AuthMiddleware).Get("/{id}", deps.PIHandler.Get)
	})

	if deps.MerchantHandler != nil {
		r.Route("/v1/merchant", func(r chi.Router) {
			r.Use(deps.AuthMiddleware)
			r.Get("/dashboard", deps.MerchantHandler.Dashboard)
			r.Get("/transactions", deps.MerchantHandler.ListTransactions)
			r.Get("/balance", deps.MerchantHandler.GetBalance)
			r.Get("/payouts", deps.MerchantHandler.ListPayouts)
			r.Get("/settings", deps.MerchantHandler.GetSettings)
			r.Put("/settings", deps.MerchantHandler.UpdateSettings)
		})
	}

	if deps.CheckoutHandler != nil {
		r.With(deps.AuthMiddleware).Post("/v1/checkout", deps.CheckoutHandler.Create)
		r.Get("/v1/checkout/{token}", deps.CheckoutHandler.Status)

		r.Get("/checkout.js", func(w http.ResponseWriter, r *http.Request) {
			data, _ := checkoutFS.ReadFile("checkout.js")
			w.Header().Set("Content-Type", "application/javascript")
			w.Write(data)
		})
	}

	return r
}
