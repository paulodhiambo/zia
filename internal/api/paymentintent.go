package api

import (
	"encoding/json"
	"net/http"

	"zia/internal/authn"
	"zia/internal/domain"
	"zia/internal/orchestrator"
	"zia/internal/service"

	"github.com/go-chi/chi/v5"
)

type PaymentIntentHandler struct {
	svc *service.PaymentIntentService
}

func NewPaymentIntentHandler(svc *service.PaymentIntentService) *PaymentIntentHandler {
	return &PaymentIntentHandler{svc: svc}
}

type createPIRequest struct {
	AmountMinor   int64  `json:"amountMinor"`
	Currency      string `json:"currency"`
	Method        string `json:"method"`
	CustomerPhone string `json:"customerPhone,omitempty"`
	CustomerEmail string `json:"customerEmail,omitempty"`
	CustomerRef   string `json:"customerRef,omitempty"`
}

type piResponse struct {
	ID            string `json:"id"`
	MerchantID    string `json:"merchantId"`
	AmountMinor   int64  `json:"amountMinor"`
	Currency      string `json:"currency"`
	Status        string `json:"status"`
	Method        string `json:"method"`
	CustomerPhone string `json:"customerPhone,omitempty"`
	CustomerEmail string `json:"customerEmail,omitempty"`
	CustomerRef   string `json:"customerRef,omitempty"`
	NextAction    *nextActionResponse `json:"nextAction,omitempty"`
	CreatedAt     string `json:"createdAt"`
	UpdatedAt     string `json:"updatedAt"`
}

type nextActionResponse struct {
	Type string `json:"type"`
	URL  string `json:"url,omitempty"`
}

func (h *PaymentIntentHandler) Create(w http.ResponseWriter, r *http.Request) {
	var env RequestEnvelope
	if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
		respondValidationError(w, r, []FieldError{
			{Field: "body", Message: "invalid JSON"},
		})
		return
	}

	var req createPIRequest
	if err := json.Unmarshal(env.PrimaryData, &req); err != nil {
		respondValidationError(w, r, []FieldError{
			{Field: "primaryData", Message: "invalid payload"},
		})
		return
	}

	if req.AmountMinor <= 0 {
		respondValidationError(w, r, []FieldError{
			{Field: "primaryData.amountMinor", Message: "must be greater than 0"},
		})
		return
	}
	if req.Currency == "" {
		respondValidationError(w, r, []FieldError{
			{Field: "primaryData.currency", Message: "required"},
		})
		return
	}
	if req.Method == "" {
		respondValidationError(w, r, []FieldError{
			{Field: "primaryData.method", Message: "required"},
		})
		return
	}

	merchantID, ok := r.Context().Value(authn.MerchantIDKey).(string)
	if !ok || merchantID == "" {
		respondError(w, r, http.StatusUnauthorized, "1002", "unauthorized")
		return
	}

	orcReq := orchestrator.CreatePIRequest{
		MerchantID:    merchantID,
		AmountMinor:   req.AmountMinor,
		Currency:      req.Currency,
		Method:        domain.PaymentMethod(req.Method),
		CustomerRef:   req.CustomerRef,
		CustomerPhone: req.CustomerPhone,
		CustomerEmail: req.CustomerEmail,
		IdempotencyKey: env.MessageID,
	}

	result, err := h.svc.Create(r.Context(), orcReq)
	if err != nil {
		respondError(w, r, http.StatusUnprocessableEntity, "1005", err.Error())
		return
	}

	resp := piResponse{
		ID:            result.PaymentIntent.ID,
		MerchantID:    result.PaymentIntent.MerchantID,
		AmountMinor:   result.PaymentIntent.AmountMinor,
		Currency:      result.PaymentIntent.Currency,
		Status:        string(result.PaymentIntent.Status),
		Method:        string(result.PaymentIntent.Method),
		CustomerPhone: result.PaymentIntent.CustomerPhone,
		CustomerEmail: result.PaymentIntent.CustomerEmail,
		CustomerRef:   result.PaymentIntent.CustomerRef,
		CreatedAt:     result.PaymentIntent.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:     result.PaymentIntent.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}

	if result.NextAction != nil {
		resp.NextAction = &nextActionResponse{
			Type: result.NextAction.Type,
			URL:  result.NextAction.URL,
		}
	}

	respond(w, r, http.StatusOK, resp)
}

func (h *PaymentIntentHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		respondError(w, r, http.StatusBadRequest, "1001", "missing payment intent id")
		return
	}

	pi, err := h.svc.GetByID(r.Context(), id)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "1003", "payment intent not found")
		return
	}

	resp := piResponse{
		ID:            pi.ID,
		MerchantID:    pi.MerchantID,
		AmountMinor:   pi.AmountMinor,
		Currency:      pi.Currency,
		Status:        string(pi.Status),
		Method:        string(pi.Method),
		CustomerPhone: pi.CustomerPhone,
		CustomerEmail: pi.CustomerEmail,
		CustomerRef:   pi.CustomerRef,
		CreatedAt:     pi.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:     pi.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}

	respond(w, r, http.StatusOK, resp)
}
