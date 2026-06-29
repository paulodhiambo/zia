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
	PortalHandler    *PortalHandler
	AuthMiddleware   func(http.Handler) http.Handler
	SessionMiddleware func(http.Handler) http.Handler
}

func NewRouter(deps Dependencies) http.Handler {
	r := chi.NewRouter()

	r.Use(Tracing)
	r.Use(RequestID)
	r.Use(ConversationID)
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

	if deps.PortalHandler != nil && deps.SessionMiddleware != nil {
		r.Route("/api/v1", func(api chi.Router) {
			api.Post("/auth/login", deps.PortalHandler.Login)
			api.Post("/auth/signup", deps.PortalHandler.Signup)
			api.Post("/auth/send-otp", deps.PortalHandler.SendOTP)
			api.Post("/auth/forgot-password", deps.PortalHandler.ForgotPassword)

			api.Group(func(auth chi.Router) {
				auth.Use(deps.SessionMiddleware)
				auth.Get("/dashboard/overview", deps.PortalHandler.DashboardOverview)
				auth.Get("/dashboard/volume", deps.PortalHandler.DashboardVolume)
				auth.Get("/transactions", deps.PortalHandler.ListTransactions)
				auth.Get("/transactions/export", deps.PortalHandler.ExportTransactionsCSV)
				auth.Get("/transactions/{id}", deps.PortalHandler.GetTransaction)
				auth.Get("/payouts", deps.PortalHandler.ListPayouts)
				auth.Post("/payouts/create", deps.PortalHandler.CreatePayout)
				auth.Get("/customers", deps.PortalHandler.ListCustomers)
				auth.Post("/customers/create", deps.PortalHandler.CreateCustomer)
				auth.Get("/customers/{id}", deps.PortalHandler.GetCustomer)
				auth.Get("/customers/{id}/charges", deps.PortalHandler.GetCustomerCharges)
				auth.Get("/team/members", deps.PortalHandler.ListTeamMembers)
				auth.Get("/team/invitations", deps.PortalHandler.ListInvitations)
				auth.Post("/team/invite", deps.PortalHandler.InviteMember)
				auth.Post("/team/invitations/{email}/resend", deps.PortalHandler.ResendInvitation)
				auth.Delete("/team/invitations/{email}/revoke", deps.PortalHandler.RevokeInvitation)
				auth.Post("/team/invitations/{token}/accept", deps.PortalHandler.AcceptInvitation)
				auth.Get("/developer/keys", deps.PortalHandler.ListAPIKeys)
				auth.Post("/developer/keys/create", deps.PortalHandler.CreateAPIKey)
				auth.Delete("/developer/keys/{id}", deps.PortalHandler.RevokeAPIKey)
				auth.Get("/developer/webhooks", deps.PortalHandler.ListWebhookEndpoints)
				auth.Post("/developer/webhooks/create", deps.PortalHandler.CreateWebhookEndpoint)
				auth.Get("/developer/webhooks/events", deps.PortalHandler.ListWebhookEvents)
				auth.Get("/notifications", deps.PortalHandler.ListNotifications)
				auth.Post("/notifications/mark-all-read", deps.PortalHandler.MarkAllNotificationsRead)
				auth.Get("/notifications/preferences", deps.PortalHandler.GetNotificationPreferences)
				auth.Post("/notifications/preferences", deps.PortalHandler.UpdateNotificationPreferences)
				auth.Get("/profile", deps.PortalHandler.GetProfile)
				auth.Post("/profile/update", deps.PortalHandler.UpdateProfile)
				auth.Get("/workspace", deps.PortalHandler.GetWorkspace)
				auth.Put("/workspace", deps.PortalHandler.UpdateWorkspace)
			})
		})
	}

	if deps.CheckoutHandler != nil {
		r.With(deps.AuthMiddleware).Post("/v1/checkout", deps.CheckoutHandler.Create)
		r.Get("/v1/checkout/{token}", deps.CheckoutHandler.Status)

		r.Get("/checkout.js", func(w http.ResponseWriter, r *http.Request) {
			data, _ := checkoutFS.ReadFile("checkout.js")
			w.Header().Set("Content-Type", "application/javascript")
			_, _ = w.Write(data)
		})
	}

	return r
}
