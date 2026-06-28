package api

import (
	"encoding/json"
	"net/http"
	"time"

	"zia/internal/authn"
	"zia/internal/domain"
	"zia/internal/orchestrator"
	"zia/internal/repository"
	"zia/internal/service"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type CheckoutHandler struct {
	svc      *service.PaymentIntentService
	checkout repository.CheckoutRepository
	piRepo   repository.PaymentIntentRepository
	logger   *zap.Logger
}

func NewCheckoutHandler(
	svc *service.PaymentIntentService,
	checkout repository.CheckoutRepository,
	piRepo repository.PaymentIntentRepository,
	logger *zap.Logger,
) *CheckoutHandler {
	return &CheckoutHandler{
		svc:      svc,
		checkout: checkout,
		piRepo:   piRepo,
		logger:   logger,
	}
}

type createCheckoutRequest struct {
	AmountMinor   int64  `json:"amountMinor"`
	Currency      string `json:"currency"`
	CustomerEmail string `json:"customerEmail"`
	CustomerPhone string `json:"customerPhone"`
	CallbackURL   string `json:"callbackUrl"`
}

func (h *CheckoutHandler) Create(w http.ResponseWriter, r *http.Request) {
	merchantID := r.Context().Value(authn.MerchantIDKey).(string)

	var req createCheckoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondValidationError(w, r, []FieldError{
			{Field: "body", Message: "invalid JSON"},
		})
		return
	}

	if req.AmountMinor <= 0 {
		respondValidationError(w, r, []FieldError{
			{Field: "amountMinor", Message: "must be positive"},
		})
		return
	}
	if req.Currency == "" {
		respondValidationError(w, r, []FieldError{
			{Field: "currency", Message: "required"},
		})
		return
	}

	piResult, err := h.svc.Create(r.Context(), buildPIRequest(merchantID, req))
	if err != nil {
		h.logger.Error("create payment intent for checkout", zap.Error(err))
		respondError(w, r, http.StatusInternalServerError, "500", "failed to create payment")
		return
	}

	now := time.Now().UTC()
	token := "cs_" + uuid.New().String()

	uiCfg, _ := json.Marshal(map[string]any{
		"amount":   req.AmountMinor,
		"currency": req.Currency,
	})

	session := &domain.CheckoutSession{
		ID:              uuid.New().String(),
		PaymentIntentID: piResult.PaymentIntent.ID,
		PublicToken:     token,
		UIConfig:        uiCfg,
		ExpiresAt:       now.Add(30 * time.Minute),
		CreatedAt:       now,
	}

	if err := h.checkout.Create(r.Context(), session); err != nil {
		h.logger.Error("create checkout session", zap.Error(err))
		respondError(w, r, http.StatusInternalServerError, "500", "failed to create checkout session")
		return
	}

	respond(w, r, http.StatusCreated, map[string]any{
		"token":          token,
		"paymentIntentId": piResult.PaymentIntent.ID,
		"expiresAt":      session.ExpiresAt,
		"status":         piResult.PaymentIntent.Status,
		"nextAction":     piResult.NextAction,
	})
}

func (h *CheckoutHandler) Status(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")

	session, err := h.checkout.GetByToken(r.Context(), token)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "404", "checkout session not found")
		return
	}

	pi, err := h.piRepo.GetByID(r.Context(), session.PaymentIntentID)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "404", "payment not found")
		return
	}

	respond(w, r, http.StatusOK, map[string]any{
		"token":          token,
		"paymentIntentId": pi.ID,
		"status":         pi.Status,
		"amountMinor":    pi.AmountMinor,
		"currency":       pi.Currency,
	})
}

func buildPIRequest(merchantID string, req createCheckoutRequest) orchestrator.CreatePIRequest {
	return orchestrator.CreatePIRequest{
		MerchantID:    merchantID,
		AmountMinor:   req.AmountMinor,
		Currency:      req.Currency,
		Method:        "mpesa_stk",
		CustomerPhone: req.CustomerPhone,
		CustomerEmail: req.CustomerEmail,
	}
}
