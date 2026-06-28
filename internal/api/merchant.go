package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"zia/internal/authn"
	"zia/internal/ledger"
	"zia/internal/repository"

	"go.uber.org/zap"
)

type MerchantHandler struct {
	merchantRepo repository.MerchantRepository
	piRepo       repository.PaymentIntentRepository
	payoutRepo   repository.PayoutRepository
	ledgerRepo   repository.LedgerRepository
	logger       *zap.Logger
}

func NewMerchantHandler(
	merchantRepo repository.MerchantRepository,
	piRepo repository.PaymentIntentRepository,
	payoutRepo repository.PayoutRepository,
	ledgerRepo repository.LedgerRepository,
	logger *zap.Logger,
) *MerchantHandler {
	return &MerchantHandler{
		merchantRepo: merchantRepo,
		piRepo:       piRepo,
		payoutRepo:   payoutRepo,
		ledgerRepo:   ledgerRepo,
		logger:       logger,
	}
}

func (h *MerchantHandler) Dashboard(w http.ResponseWriter, r *http.Request) {
	merchantID := r.Context().Value(authn.MerchantIDKey).(string)

	merchant, err := h.merchantRepo.GetByID(r.Context(), merchantID)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "404", "merchant not found")
		return
	}

	pis, err := h.piRepo.ListByMerchant(r.Context(), merchantID, 5, 0)
	if err != nil {
		h.logger.Error("list transactions", zap.Error(err))
		respondError(w, r, http.StatusInternalServerError, "500", "internal error")
		return
	}

	availableBalance, _ := h.ledgerRepo.Balance(r.Context(), ledger.MerchantAvailable(merchantID))
	inTransitBalance, _ := h.ledgerRepo.Balance(r.Context(), ledger.MerchantInTransit(merchantID))

	respond(w, r, http.StatusOK, map[string]any{
		"merchant": map[string]any{
			"id":              merchant.ID,
			"legalName":       merchant.LegalName,
			"country":         merchant.Country,
			"defaultCurrency": merchant.DefaultCurrency,
			"status":          merchant.Status,
		},
		"balances": map[string]int64{
			"available": availableBalance,
			"inTransit": inTransitBalance,
		},
		"recentTransactions": pis,
	})
}

func (h *MerchantHandler) ListTransactions(w http.ResponseWriter, r *http.Request) {
	merchantID := r.Context().Value(authn.MerchantIDKey).(string)

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}

	pis, err := h.piRepo.ListByMerchant(r.Context(), merchantID, limit, offset)
	if err != nil {
		h.logger.Error("list transactions", zap.Error(err))
		respondError(w, r, http.StatusInternalServerError, "500", "internal error")
		return
	}

	respond(w, r, http.StatusOK, map[string]any{
		"transactions": pis,
		"limit":       limit,
		"offset":      offset,
	})
}

func (h *MerchantHandler) GetBalance(w http.ResponseWriter, r *http.Request) {
	merchantID := r.Context().Value(authn.MerchantIDKey).(string)

	available, _ := h.ledgerRepo.Balance(r.Context(), ledger.MerchantAvailable(merchantID))
	inTransit, _ := h.ledgerRepo.Balance(r.Context(), ledger.MerchantInTransit(merchantID))

	respond(w, r, http.StatusOK, map[string]any{
		"available": available,
		"inTransit": inTransit,
	})
}

func (h *MerchantHandler) ListPayouts(w http.ResponseWriter, r *http.Request) {
	merchantID := r.Context().Value(authn.MerchantIDKey).(string)

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}

	payouts, err := h.payoutRepo.ListByMerchant(r.Context(), merchantID, limit, offset)
	if err != nil {
		h.logger.Error("list payouts", zap.Error(err))
		respondError(w, r, http.StatusInternalServerError, "500", "internal error")
		return
	}

	respond(w, r, http.StatusOK, map[string]any{
		"payouts": payouts,
		"limit":  limit,
		"offset": offset,
	})
}

type updateSettingsRequest struct {
	WebhookURL string `json:"webhookUrl"`
}

func (h *MerchantHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	merchantID := r.Context().Value(authn.MerchantIDKey).(string)

	var env RequestEnvelope
	if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
		respondValidationError(w, r, []FieldError{
			{Field: "body", Message: "invalid JSON"},
		})
		return
	}

	var req updateSettingsRequest
	if err := json.Unmarshal(env.PrimaryData, &req); err != nil {
		respondValidationError(w, r, []FieldError{
			{Field: "primaryData", Message: "invalid payload"},
		})
		return
	}

	merchant, err := h.merchantRepo.GetByID(r.Context(), merchantID)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "404", "merchant not found")
		return
	}

	type settings struct {
		WebhookURL string `json:"webhook_url"`
	}
	s := settings(req)

	data, _ := json.Marshal(s)
	merchant.SettlementConfig = data

	respond(w, r, http.StatusOK, map[string]string{
		"status":     "updated",
		"webhookUrl": req.WebhookURL,
	})
}

func (h *MerchantHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	merchantID := r.Context().Value(authn.MerchantIDKey).(string)

	merchant, err := h.merchantRepo.GetByID(r.Context(), merchantID)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "404", "merchant not found")
		return
	}

	var webhookURL string
	if merchant.SettlementConfig != nil {
		var cfg struct {
			WebhookURL string `json:"webhook_url"`
		}
		if err := json.Unmarshal(merchant.SettlementConfig, &cfg); err == nil {
			webhookURL = cfg.WebhookURL
		}
	}

	respond(w, r, http.StatusOK, map[string]any{
		"merchant": merchant,
		"settings": map[string]string{
			"webhookUrl": webhookURL,
		},
	})
}
