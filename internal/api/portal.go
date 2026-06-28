package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"zia/internal/domain"
	"zia/internal/ledger"
	"zia/internal/repository"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

type PortalHandler struct {
	userRepo        repository.UserRepository
	sessionRepo     repository.SessionRepository
	merchantRepo    repository.MerchantRepository
	piRepo          repository.PaymentIntentRepository
	payoutRepo      repository.PayoutRepository
	ledgerRepo      repository.LedgerRepository
	customerRepo    repository.CustomerRepository
	teamMemberRepo  repository.TeamMemberRepository
	teamInviteRepo  repository.TeamInvitationRepository
	webhookEPRepo   repository.WebhookEndpointRepository
	webhookEventRepo repository.WebhookEventRepository
	notifRepo       repository.NotificationRepository
	logger          *zap.Logger
}

type portalCtxKey string

const (
	portalUserID     portalCtxKey = "portal_user_id"
	portalMerchantID portalCtxKey = "portal_merchant_id"
)

// PortalAuth validates the Bearer session token and injects both userID and
// merchantID into the request context so handlers never need a DB round-trip
// to resolve the merchant.
func PortalAuth(repo repository.SessionRepository) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
				respondError(w, r, http.StatusUnauthorized, "1002", "Authentication failed")
				return
			}
			token := strings.TrimPrefix(auth, "Bearer ")
			session, err := repo.GetByToken(r.Context(), token)
			if err != nil {
				respondError(w, r, http.StatusUnauthorized, "1002", "Invalid email or password")
				return
			}
			ctx := context.WithValue(r.Context(), portalUserID, session.UserID)
			ctx = context.WithValue(ctx, portalMerchantID, session.MerchantID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func NewPortalHandler(
	userRepo repository.UserRepository,
	sessionRepo repository.SessionRepository,
	merchantRepo repository.MerchantRepository,
	piRepo repository.PaymentIntentRepository,
	payoutRepo repository.PayoutRepository,
	ledgerRepo repository.LedgerRepository,
	customerRepo repository.CustomerRepository,
	teamMemberRepo repository.TeamMemberRepository,
	teamInviteRepo repository.TeamInvitationRepository,
	webhookEPRepo repository.WebhookEndpointRepository,
	webhookEventRepo repository.WebhookEventRepository,
	notifRepo repository.NotificationRepository,
	logger *zap.Logger,
) *PortalHandler {
	return &PortalHandler{
		userRepo:         userRepo,
		sessionRepo:      sessionRepo,
		merchantRepo:     merchantRepo,
		piRepo:           piRepo,
		payoutRepo:       payoutRepo,
		ledgerRepo:       ledgerRepo,
		customerRepo:     customerRepo,
		teamMemberRepo:   teamMemberRepo,
		teamInviteRepo:   teamInviteRepo,
		webhookEPRepo:    webhookEPRepo,
		webhookEventRepo: webhookEventRepo,
		notifRepo:        notifRepo,
		logger:           logger,
	}
}

func (h *PortalHandler) getUserID(r *http.Request) string {
	id, _ := r.Context().Value(portalUserID).(string)
	return id
}

func (h *PortalHandler) getMerchantID(r *http.Request) string {
	id, _ := r.Context().Value(portalMerchantID).(string)
	return id
}

// --- auth ---

func (h *PortalHandler) Login(w http.ResponseWriter, r *http.Request) {
	var env RequestEnvelope
	if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
		respondPortalError(w, r, http.StatusBadRequest, "1001", "Invalid request")
		return
	}
	var creds struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(env.PrimaryData, &creds); err != nil || creds.Email == "" || creds.Password == "" {
		respondPortalError(w, r, http.StatusBadRequest, "1001", "Email and password are required")
		return
	}

	user, err := h.userRepo.GetByEmailGlobal(r.Context(), creds.Email)
	if err != nil {
		respondPortalError(w, r, http.StatusUnauthorized, "1002", "Invalid email or password")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(creds.Password)); err != nil {
		respondPortalError(w, r, http.StatusUnauthorized, "1002", "Invalid email or password")
		return
	}

	session := &domain.Session{
		ID:        uuid.New().String(),
		UserID:    user.ID,
		Token:     uuid.New().String(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
		CreatedAt: time.Now(),
	}
	if err := h.sessionRepo.Create(r.Context(), session); err != nil {
		h.logger.Error("session create", zap.Error(err))
		respondPortalError(w, r, http.StatusInternalServerError, "1007", "Internal error")
		return
	}

	respond(w, r, http.StatusOK, map[string]any{
		"token": session.Token,
		"user": map[string]string{
			"id":    user.ID,
			"name":  user.Name,
			"email": user.Email,
			"role":  user.Role,
			"title": user.Title,
		},
	})
}

func (h *PortalHandler) Signup(w http.ResponseWriter, r *http.Request) {
	var env RequestEnvelope
	if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
		respondPortalError(w, r, http.StatusBadRequest, "1001", "Invalid request")
		return
	}
	var data struct {
		Name     string `json:"name"`
		Role     string `json:"role"`
		Email    string `json:"email"`
		Company  string `json:"company"`
		Country  string `json:"country"`
		Currency string `json:"currency"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(env.PrimaryData, &data); err != nil {
		respondPortalError(w, r, http.StatusBadRequest, "1001", "Invalid signup data")
		return
	}
	if data.Name == "" || data.Email == "" || data.Password == "" {
		respondPortalError(w, r, http.StatusBadRequest, "1001", "Name, email and password are required")
		return
	}
	if data.Country == "" {
		data.Country = "KE"
	}
	if data.Currency == "" {
		data.Currency = "KES"
	}

	merchantID := uuid.New().String()
	if err := h.merchantRepo.Create(r.Context(), &domain.Merchant{
		ID:              merchantID,
		LegalName:       data.Company,
		Country:         data.Country,
		DefaultCurrency: data.Currency,
		Status:          domain.MerchantActive,
		CreatedAt:       time.Now(),
	}); err != nil {
		h.logger.Error("merchant create", zap.Error(err))
		respondPortalError(w, r, http.StatusInternalServerError, "1007", "Failed to create workspace")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(data.Password), bcrypt.DefaultCost)
	if err != nil {
		respondPortalError(w, r, http.StatusInternalServerError, "1007", "Internal error")
		return
	}
	user := &domain.User{
		ID:           uuid.New().String(),
		MerchantID:   merchantID,
		Name:         data.Name,
		Email:        data.Email,
		PasswordHash: string(hash),
		Role:         data.Role,
		Title:        data.Role,
		CreatedAt:    time.Now(),
	}
	if err := h.userRepo.Create(r.Context(), user); err != nil {
		h.logger.Error("user create", zap.Error(err))
		respondPortalError(w, r, http.StatusInternalServerError, "1007", "Failed to create user")
		return
	}

	respond(w, r, http.StatusOK, map[string]bool{"success": true})
}

func (h *PortalHandler) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	var env RequestEnvelope
	if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
		respondPortalError(w, r, http.StatusBadRequest, "1001", "Invalid request")
		return
	}
	var data struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(env.PrimaryData, &data); err != nil || data.Email == "" {
		respondPortalError(w, r, http.StatusBadRequest, "1001", "Valid email is required")
		return
	}
	// Always return success to prevent user enumeration; email delivery is
	// handled by the notification service outside this handler.
	respond(w, r, http.StatusOK, map[string]bool{"success": true})
}

// --- dashboard ---

func (h *PortalHandler) DashboardOverview(w http.ResponseWriter, r *http.Request) {
	merchantID := h.getMerchantID(r)

	pis, _ := h.piRepo.ListByMerchant(r.Context(), merchantID, 500, 0)

	todayStart := time.Now().UTC().Truncate(24 * time.Hour)
	var todayVolume int64
	var successCount int
	for _, pi := range pis {
		if pi.CreatedAt.After(todayStart) {
			todayVolume += pi.AmountMinor
		}
		if pi.Status == domain.PISucceeded {
			successCount++
		}
	}

	available, _ := h.ledgerRepo.Balance(r.Context(), ledger.MerchantAvailable(merchantID))
	payoutsList, _ := h.payoutRepo.ListByMerchant(r.Context(), merchantID, 100, 0)
	var pendingPayouts int64
	for _, p := range payoutsList {
		if p.Status == domain.PayoutPending {
			pendingPayouts += p.AmountMinor
		}
	}

	respond(w, r, http.StatusOK, map[string]any{
		"treasuryBalance":    float64(available) / 100.0,
		"todayVolume":        float64(todayVolume) / 100.0,
		"successfulPayments": successCount,
		"pendingPayouts":     float64(pendingPayouts) / 100.0,
		"checklist": []map[string]any{
			{"task": "Verify business entity", "status": "Done", "completed": true},
			{"task": "Connect payment method", "status": "Done", "completed": true},
			{"task": "Invite team members", "status": "Pending", "completed": false},
			{"task": "Set up webhook", "status": "Pending", "completed": false},
			{"task": "API integration live", "status": "Pending", "completed": false},
		},
	})
}

// --- transactions ---

func (h *PortalHandler) ListTransactions(w http.ResponseWriter, r *http.Request) {
	merchantID := h.getMerchantID(r)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	pis, err := h.piRepo.ListByMerchant(r.Context(), merchantID, limit, offset)
	if err != nil {
		h.logger.Error("list transactions", zap.Error(err))
		respondPortalError(w, r, http.StatusInternalServerError, "1007", "Internal error")
		return
	}

	txs := make([]map[string]any, 0, len(pis))
	for _, pi := range pis {
		txs = append(txs, map[string]any{
			"id":           pi.ID,
			"date":         pi.CreatedAt.Format("Jan 2 · 15:04"),
			"counterparty": pi.CustomerRef,
			"method":       string(pi.Method),
			"amount":       fmt.Sprintf("%.2f", float64(pi.AmountMinor)/100.0),
			"currency":     pi.Currency,
			"status":       mapStatus(pi.Status),
		})
	}

	respond(w, r, http.StatusOK, map[string]any{"transactions": txs})
}

// --- payouts ---

func (h *PortalHandler) ListPayouts(w http.ResponseWriter, r *http.Request) {
	merchantID := h.getMerchantID(r)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	payouts, err := h.payoutRepo.ListByMerchant(r.Context(), merchantID, limit, offset)
	if err != nil {
		h.logger.Error("list payouts", zap.Error(err))
		respondPortalError(w, r, http.StatusInternalServerError, "1007", "Internal error")
		return
	}

	resp := make([]map[string]any, 0, len(payouts))
	for _, p := range payouts {
		status := "Pending"
		if p.Status == domain.PayoutSucceeded {
			status = "Succeeded"
		} else if p.Status == domain.PayoutFailed {
			status = "Failed"
		}
		resp = append(resp, map[string]any{
			"id":          p.ID,
			"date":        p.CreatedAt.Format("Jan 2 · 15:04"),
			"source":      "Operating · " + p.Currency,
			"destination": "Bank · " + p.Rail,
			"bank":        p.Rail,
			"amount":      fmt.Sprintf("%.2f", float64(p.AmountMinor)/100.0),
			"currency":    p.Currency,
			"status":      status,
		})
	}

	respond(w, r, http.StatusOK, map[string]any{"payouts": resp})
}

func (h *PortalHandler) CreatePayout(w http.ResponseWriter, r *http.Request) {
	merchantID := h.getMerchantID(r)
	var env RequestEnvelope
	if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
		respondPortalError(w, r, http.StatusBadRequest, "1001", "Invalid request")
		return
	}
	var data struct {
		Source      string  `json:"source"`
		Bank        string  `json:"bank"`
		Routing     string  `json:"routing"`
		Account     string  `json:"account"`
		Amount      float64 `json:"amount"`
		Currency    string  `json:"currency"`
		Description string  `json:"description"`
	}
	if err := json.Unmarshal(env.PrimaryData, &data); err != nil {
		respondPortalError(w, r, http.StatusBadRequest, "1001", "Invalid payout data")
		return
	}
	if data.Amount <= 0 {
		respondPortalError(w, r, http.StatusBadRequest, "1001", "Amount must be greater than zero")
		return
	}
	if data.Currency == "" {
		data.Currency = "KES"
	}

	last4 := data.Account
	if len(last4) > 4 {
		last4 = last4[len(last4)-4:]
	}

	payout := &domain.Payout{
		ID:          uuid.New().String(),
		MerchantID:  merchantID,
		AmountMinor: int64(data.Amount * 100),
		Currency:    data.Currency,
		Rail:        data.Bank,
		Status:      domain.PayoutPending,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := h.payoutRepo.Create(r.Context(), payout); err != nil {
		h.logger.Error("create payout", zap.Error(err))
		respondPortalError(w, r, http.StatusInternalServerError, "1007", "Failed to create payout")
		return
	}

	respond(w, r, http.StatusOK, map[string]any{
		"id":          payout.ID,
		"date":        payout.CreatedAt.Format("Jan 2 · 15:04"),
		"source":      data.Source,
		"destination": data.Bank + " •••• " + last4,
		"bank":        data.Bank,
		"amount":      fmt.Sprintf("%.2f", data.Amount),
		"currency":    payout.Currency,
		"status":      "Pending",
	})
}

// --- customers ---

func (h *PortalHandler) ListCustomers(w http.ResponseWriter, r *http.Request) {
	merchantID := h.getMerchantID(r)
	search := r.URL.Query().Get("search")
	status := r.URL.Query().Get("status")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	customers, err := h.customerRepo.ListByMerchant(r.Context(), merchantID, search, status, limit, offset)
	if err != nil {
		h.logger.Error("list customers", zap.Error(err))
		respondPortalError(w, r, http.StatusInternalServerError, "1007", "Internal error")
		return
	}

	resp := make([]map[string]any, 0, len(customers))
	for _, c := range customers {
		resp = append(resp, formatCustomer(&c))
	}
	respond(w, r, http.StatusOK, map[string]any{"customers": resp})
}

func (h *PortalHandler) CreateCustomer(w http.ResponseWriter, r *http.Request) {
	merchantID := h.getMerchantID(r)
	var env RequestEnvelope
	if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
		respondPortalError(w, r, http.StatusBadRequest, "1001", "Invalid request")
		return
	}
	var data struct {
		Name          string  `json:"name"`
		Company       string  `json:"company"`
		Email         string  `json:"email"`
		Phone         string  `json:"phone"`
		Location      string  `json:"location"`
		InitialVolume float64 `json:"initialVolume"`
	}
	if err := json.Unmarshal(env.PrimaryData, &data); err != nil {
		respondPortalError(w, r, http.StatusBadRequest, "1001", "Invalid customer data")
		return
	}
	if data.Email == "" {
		respondPortalError(w, r, http.StatusBadRequest, "1001", "Email is required")
		return
	}

	c := &domain.Customer{
		ID:          uuid.New().String(),
		MerchantID:  merchantID,
		Name:        data.Name,
		Company:     data.Company,
		Email:       data.Email,
		Phone:       data.Phone,
		Location:    data.Location,
		VolumeMinor: int64(data.InitialVolume * 100),
		Status:      "Active",
		JoinedAt:    time.Now(),
		CreatedAt:   time.Now(),
	}
	if err := h.customerRepo.Create(r.Context(), c); err != nil {
		h.logger.Error("create customer", zap.Error(err))
		respondPortalError(w, r, http.StatusBadRequest, "1001", "Duplicate or invalid customer")
		return
	}
	respond(w, r, http.StatusOK, formatCustomer(c))
}

func (h *PortalHandler) GetCustomer(w http.ResponseWriter, r *http.Request) {
	c, err := h.customerRepo.GetByID(r.Context(), r.PathValue("id"))
	if err != nil {
		respondPortalError(w, r, http.StatusNotFound, "1003", "Customer not found")
		return
	}
	respond(w, r, http.StatusOK, formatCustomer(c))
}

func (h *PortalHandler) GetCustomerCharges(w http.ResponseWriter, r *http.Request) {
	merchantID := h.getMerchantID(r)
	customerID := r.PathValue("id")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	// Fetch a larger page and filter by CustomerRef matching the customer ID.
	// A dedicated query index on customer_ref is the long-term solution.
	pis, err := h.piRepo.ListByMerchant(r.Context(), merchantID, 500, 0)
	if err != nil {
		h.logger.Error("list customer charges", zap.Error(err))
		respondPortalError(w, r, http.StatusInternalServerError, "1007", "Internal error")
		return
	}

	charges := make([]map[string]any, 0)
	skipped := 0
	for _, pi := range pis {
		if pi.CustomerRef != customerID {
			continue
		}
		if skipped < offset {
			skipped++
			continue
		}
		if len(charges) >= limit {
			break
		}
		charges = append(charges, map[string]any{
			"id":       pi.ID,
			"date":     pi.CreatedAt.Format("Jan 2 · 15:04"),
			"amount":   fmt.Sprintf("%.2f", float64(pi.AmountMinor)/100.0),
			"currency": pi.Currency,
			"method":   string(pi.Method),
			"status":   mapStatus(pi.Status),
		})
	}

	respond(w, r, http.StatusOK, map[string]any{"charges": charges})
}

// --- team ---

func (h *PortalHandler) ListTeamMembers(w http.ResponseWriter, r *http.Request) {
	merchantID := h.getMerchantID(r)
	members, err := h.teamMemberRepo.ListByMerchant(r.Context(), merchantID)
	if err != nil {
		h.logger.Error("list team members", zap.Error(err))
		respondPortalError(w, r, http.StatusInternalServerError, "1007", "Internal error")
		return
	}
	resp := make([]map[string]any, 0, len(members))
	for _, m := range members {
		last := "Never"
		if m.LastActive != nil {
			last = m.LastActive.Format("Jan 2")
		}
		resp = append(resp, map[string]any{
			"name":     m.Name,
			"email":    m.Email,
			"role":     m.Role,
			"last":     last,
			"initials": m.Initials,
		})
	}
	respond(w, r, http.StatusOK, map[string]any{"members": resp})
}

func (h *PortalHandler) ListInvitations(w http.ResponseWriter, r *http.Request) {
	merchantID := h.getMerchantID(r)
	invs, err := h.teamInviteRepo.ListByMerchant(r.Context(), merchantID)
	if err != nil {
		h.logger.Error("list invitations", zap.Error(err))
		respondPortalError(w, r, http.StatusInternalServerError, "1007", "Internal error")
		return
	}
	resp := make([]map[string]any, 0, len(invs))
	for _, inv := range invs {
		resp = append(resp, map[string]any{
			"email":   inv.Email,
			"role":    inv.Role,
			"invited": inv.CreatedAt.Format("Jan 2"),
		})
	}
	respond(w, r, http.StatusOK, map[string]any{"invitations": resp})
}

func (h *PortalHandler) InviteMember(w http.ResponseWriter, r *http.Request) {
	merchantID := h.getMerchantID(r)
	var env RequestEnvelope
	if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
		respondPortalError(w, r, http.StatusBadRequest, "1001", "Invalid request")
		return
	}
	var data struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if err := json.Unmarshal(env.PrimaryData, &data); err != nil || data.Email == "" || data.Role == "" {
		respondPortalError(w, r, http.StatusBadRequest, "1001", "Email and role are required")
		return
	}

	if m, _ := h.teamMemberRepo.GetByEmail(r.Context(), merchantID, data.Email); m != nil {
		respondPortalError(w, r, http.StatusBadRequest, "1001", "User is already a team member")
		return
	}
	if inv, _ := h.teamInviteRepo.GetByEmail(r.Context(), merchantID, data.Email); inv != nil {
		respondPortalError(w, r, http.StatusBadRequest, "1001", "Invitation already sent")
		return
	}

	inv := &domain.TeamInvitation{
		ID:         uuid.New().String(),
		MerchantID: merchantID,
		Email:      data.Email,
		Role:       data.Role,
		Token:      uuid.New().String(),
		ExpiresAt:  time.Now().Add(7 * 24 * time.Hour),
		CreatedAt:  time.Now(),
	}
	if err := h.teamInviteRepo.Create(r.Context(), inv); err != nil {
		h.logger.Error("create invitation", zap.Error(err))
		respondPortalError(w, r, http.StatusInternalServerError, "1007", "Failed to send invitation")
		return
	}
	respond(w, r, http.StatusOK, map[string]any{
		"email":   inv.Email,
		"role":    inv.Role,
		"invited": inv.CreatedAt.Format("Jan 2"),
	})
}

func (h *PortalHandler) ResendInvitation(w http.ResponseWriter, r *http.Request) {
	merchantID := h.getMerchantID(r)
	email := r.PathValue("email")
	inv, err := h.teamInviteRepo.GetByEmail(r.Context(), merchantID, email)
	if err != nil {
		respondPortalError(w, r, http.StatusNotFound, "1003", "No active invitation found")
		return
	}
	// Refresh token and expiry, then upsert (avoids a duplicate-key error).
	inv.Token = uuid.New().String()
	inv.ExpiresAt = time.Now().Add(7 * 24 * time.Hour)
	inv.CreatedAt = time.Now()
	if err := h.teamInviteRepo.Upsert(r.Context(), inv); err != nil {
		h.logger.Error("resend invitation", zap.Error(err))
		respondPortalError(w, r, http.StatusInternalServerError, "1007", "Failed to resend")
		return
	}
	respond(w, r, http.StatusOK, map[string]bool{"success": true})
}

func (h *PortalHandler) RevokeInvitation(w http.ResponseWriter, r *http.Request) {
	merchantID := h.getMerchantID(r)
	if err := h.teamInviteRepo.DeleteByEmail(r.Context(), merchantID, r.PathValue("email")); err != nil {
		h.logger.Error("revoke invitation", zap.Error(err))
		respondPortalError(w, r, http.StatusNotFound, "1003", "No invitation found")
		return
	}
	respond(w, r, http.StatusOK, map[string]bool{"success": true})
}

// --- developer ---

func (h *PortalHandler) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	merchantID := h.getMerchantID(r)
	keys, err := h.merchantRepo.ListAPIKeys(r.Context(), merchantID)
	if err != nil {
		h.logger.Error("list api keys", zap.Error(err))
		respondPortalError(w, r, http.StatusInternalServerError, "1007", "Internal error")
		return
	}
	resp := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		resp = append(resp, map[string]any{
			"id":          k.ID,
			"name":        k.Name,
			"prefix":      k.KeyPrefix,
			"environment": k.Environment,
			"createdAt":   k.CreatedAt.Format("Jan 2, 2006"),
		})
	}
	respond(w, r, http.StatusOK, map[string]any{"keys": resp})
}

func (h *PortalHandler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	merchantID := h.getMerchantID(r)
	var env RequestEnvelope
	if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
		respondPortalError(w, r, http.StatusBadRequest, "1001", "Invalid request")
		return
	}
	var data struct {
		Name string `json:"name"`
		Env  string `json:"env"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(env.PrimaryData, &data); err != nil || data.Name == "" {
		respondPortalError(w, r, http.StatusBadRequest, "1001", "Key name is required")
		return
	}
	if data.Env == "" {
		data.Env = "live"
	}

	prefix := "sk"
	if data.Type == "Publishable" {
		prefix = "pk"
	}
	rawBytes := make([]byte, 24)
	if _, err := rand.Read(rawBytes); err != nil {
		respondPortalError(w, r, http.StatusInternalServerError, "1007", "Internal error")
		return
	}
	rawKey := fmt.Sprintf("%s_%s_%s", prefix, data.Env, hex.EncodeToString(rawBytes))
	hash := sha256.Sum256([]byte(rawKey))

	keyPrefix := rawKey
	if len(keyPrefix) > 12 {
		keyPrefix = keyPrefix[:12]
	}
	apiKey := &domain.APIKey{
		ID:          uuid.New().String(),
		MerchantID:  merchantID,
		Name:        data.Name,
		KeyHash:     hex.EncodeToString(hash[:]),
		KeyPrefix:   keyPrefix,
		Environment: data.Env,
		CreatedAt:   time.Now(),
	}
	if err := h.merchantRepo.CreateAPIKey(r.Context(), apiKey); err != nil {
		h.logger.Error("create api key", zap.Error(err))
		respondPortalError(w, r, http.StatusInternalServerError, "1007", "Failed to create API key")
		return
	}

	// rawKey is returned once and never stored in plaintext.
	respond(w, r, http.StatusOK, map[string]any{
		"id":          apiKey.ID,
		"name":        apiKey.Name,
		"key":         rawKey,
		"prefix":      apiKey.KeyPrefix,
		"environment": apiKey.Environment,
		"createdAt":   apiKey.CreatedAt.Format("Jan 2, 2006"),
	})
}

func (h *PortalHandler) ListWebhookEndpoints(w http.ResponseWriter, r *http.Request) {
	merchantID := h.getMerchantID(r)
	eps, err := h.webhookEPRepo.ListByMerchant(r.Context(), merchantID)
	if err != nil {
		h.logger.Error("list webhook endpoints", zap.Error(err))
		respondPortalError(w, r, http.StatusInternalServerError, "1007", "Internal error")
		return
	}
	resp := make([]map[string]any, 0, len(eps))
	for _, ep := range eps {
		resp = append(resp, map[string]any{
			"id":     ep.ID,
			"url":    ep.URL,
			"events": ep.Events,
			"status": ep.Status,
		})
	}
	respond(w, r, http.StatusOK, map[string]any{"webhooks": resp})
}

func (h *PortalHandler) CreateWebhookEndpoint(w http.ResponseWriter, r *http.Request) {
	merchantID := h.getMerchantID(r)
	var env RequestEnvelope
	if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
		respondPortalError(w, r, http.StatusBadRequest, "1001", "Invalid request")
		return
	}
	var data struct {
		URL    string `json:"url"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(env.PrimaryData, &data); err != nil || data.URL == "" {
		respondPortalError(w, r, http.StatusBadRequest, "1001", "URL is required")
		return
	}
	if data.Status == "" {
		data.Status = "active"
	}

	ep := &domain.WebhookEndpoint{
		ID:         uuid.New().String(),
		MerchantID: merchantID,
		URL:        data.URL,
		Events:     0,
		Status:     data.Status,
		CreatedAt:  time.Now(),
	}
	if err := h.webhookEPRepo.Create(r.Context(), ep); err != nil {
		h.logger.Error("create webhook endpoint", zap.Error(err))
		respondPortalError(w, r, http.StatusBadRequest, "1001", "Invalid URL or duplicate endpoint")
		return
	}
	respond(w, r, http.StatusOK, map[string]any{
		"id":     ep.ID,
		"url":    ep.URL,
		"events": ep.Events,
		"status": ep.Status,
	})
}

// --- webhook events ---

func (h *PortalHandler) ListWebhookEvents(w http.ResponseWriter, r *http.Request) {
	merchantID := h.getMerchantID(r)
	evts, err := h.webhookEventRepo.ListByMerchant(r.Context(), merchantID, 50)
	if err != nil {
		h.logger.Error("list webhook events", zap.Error(err))
		respondPortalError(w, r, http.StatusInternalServerError, "1007", "Internal error")
		return
	}
	resp := make([]map[string]any, 0, len(evts))
	for _, e := range evts {
		resp = append(resp, map[string]any{
			"id":                e.ID,
			"psp":               e.PSP,
			"eventType":         e.EventType,
			"pspReference":      e.PSPReference,
			"processingStatus":  e.ProcessingStatus,
			"receivedAt":        e.ReceivedAt,
		})
	}
	respond(w, r, http.StatusOK, map[string]any{"events": resp})
}

// --- notifications ---

func (h *PortalHandler) ListNotifications(w http.ResponseWriter, r *http.Request) {
	merchantID := h.getMerchantID(r)
	notifs, err := h.notifRepo.ListByMerchant(r.Context(), merchantID)
	if err != nil {
		h.logger.Error("list notifications", zap.Error(err))
		respondPortalError(w, r, http.StatusInternalServerError, "1007", "Internal error")
		return
	}
	resp := make([]map[string]any, 0, len(notifs))
	for _, n := range notifs {
		resp = append(resp, map[string]any{
			"id":       n.ID,
			"tone":     n.Tone,
			"title":    n.Title,
			"body":     n.Body,
			"time":     durationAgo(n.CreatedAt),
			"unread":   n.Unread,
			"category": n.Category,
		})
	}
	respond(w, r, http.StatusOK, map[string]any{"notifications": resp})
}

func (h *PortalHandler) MarkAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	merchantID := h.getMerchantID(r)
	if err := h.notifRepo.MarkAllRead(r.Context(), merchantID); err != nil {
		h.logger.Error("mark notifications read", zap.Error(err))
		respondPortalError(w, r, http.StatusInternalServerError, "1007", "Internal error")
		return
	}
	respond(w, r, http.StatusOK, map[string]bool{"success": true})
}

func (h *PortalHandler) GetNotificationPreferences(w http.ResponseWriter, r *http.Request) {
	merchantID := h.getMerchantID(r)
	prefs, err := h.notifRepo.GetPreferences(r.Context(), merchantID)
	if err != nil {
		h.logger.Error("get notification preferences", zap.Error(err))
		respondPortalError(w, r, http.StatusInternalServerError, "1007", "Internal error")
		return
	}
	var data map[string]any
	json.Unmarshal(prefs.Preferences, &data)
	respond(w, r, http.StatusOK, map[string]any{"preferences": data})
}

func (h *PortalHandler) UpdateNotificationPreferences(w http.ResponseWriter, r *http.Request) {
	merchantID := h.getMerchantID(r)

	var env RequestEnvelope
	if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
		respondPortalError(w, r, http.StatusBadRequest, "1001", "Invalid request")
		return
	}

	var incoming map[string]any
	if err := json.Unmarshal(env.PrimaryData, &incoming); err != nil {
		respondPortalError(w, r, http.StatusBadRequest, "1001", "Invalid primaryData")
		return
	}

	existingPrefs, err := h.notifRepo.GetPreferences(r.Context(), merchantID)
	var merged map[string]any
	if err == nil {
		json.Unmarshal(existingPrefs.Preferences, &merged)
	} else {
		merged = make(map[string]any)
	}
	deepMerge(merged, incoming)

	mergedJSON, _ := json.Marshal(merged)
	if err := h.notifRepo.UpsertPreferences(r.Context(), merchantID, mergedJSON); err != nil {
		h.logger.Error("upsert notification preferences", zap.Error(err))
		respondPortalError(w, r, http.StatusInternalServerError, "1007", "Failed to save preferences")
		return
	}

	respond(w, r, http.StatusOK, map[string]any{
		"success":     true,
		"preferences": merged,
	})
}

// --- profile ---

func (h *PortalHandler) GetProfile(w http.ResponseWriter, r *http.Request) {
	user, err := h.userRepo.GetByID(r.Context(), h.getUserID(r))
	if err != nil {
		respondPortalError(w, r, http.StatusNotFound, "1003", "User not found")
		return
	}
	respond(w, r, http.StatusOK, buildProfileResponse(user))
}

func (h *PortalHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	userID := h.getUserID(r)
	var env RequestEnvelope
	if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
		respondPortalError(w, r, http.StatusBadRequest, "1001", "Invalid request")
		return
	}
	var data struct {
		Name  string `json:"name"`
		Title string `json:"title"`
		Phone string `json:"phone"`
	}
	if err := json.Unmarshal(env.PrimaryData, &data); err != nil {
		respondPortalError(w, r, http.StatusBadRequest, "1001", "Invalid profile data")
		return
	}
	if err := h.userRepo.UpdateProfile(r.Context(), userID, data.Name, data.Title, data.Phone); err != nil {
		h.logger.Error("update profile", zap.Error(err))
		respondPortalError(w, r, http.StatusInternalServerError, "1007", "Failed to update")
		return
	}
	user, err := h.userRepo.GetByID(r.Context(), userID)
	if err != nil {
		respondPortalError(w, r, http.StatusInternalServerError, "1007", "Internal error")
		return
	}
	respond(w, r, http.StatusOK, buildProfileResponse(user))
}

// --- helpers ---

func formatCustomer(c *domain.Customer) map[string]any {
	return map[string]any{
		"id":            c.ID,
		"name":          c.Name,
		"company":       c.Company,
		"email":         c.Email,
		"phone":         c.Phone,
		"location":      c.Location,
		"volume":        fmt.Sprintf("%.2f", float64(c.VolumeMinor)/100.0),
		"ltv":           fmt.Sprintf("%.2f", float64(c.LTVMinor)/100.0),
		"joined":        c.JoinedAt.Format("Jan 2, 2006"),
		"status":        c.Status,
		"paymentMethod": c.PaymentMethod,
	}
}

func buildProfileResponse(user *domain.User) map[string]any {
	return map[string]any{
		"user": map[string]string{
			"id":    user.ID,
			"name":  user.Name,
			"title": user.Title,
			"email": user.Email,
			"phone": user.Phone,
			"role":  user.Role,
		},
		"security": map[string]any{
			"twoFactorEnabled":    false,
			"hardwareKeysCount":   0,
			"activeSessionsCount": 1,
		},
		"preferences": map[string]any{
			"dailyDigest":          true,
			"alertOnDisputes":      true,
			"weeklyTreasuryReport": false,
			"betaFeatures":         false,
		},
	}
}

func mapStatus(s domain.PaymentIntentStatus) string {
	switch s {
	case domain.PISucceeded:
		return "Succeeded"
	case domain.PIFailed:
		return "Failed"
	case domain.PIRefunded, domain.PIPartiallyRefunded:
		return "Refunded"
	default:
		return "Pending"
	}
}

func durationAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "Just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1 min ago"
		}
		return fmt.Sprintf("%d min ago", m)
	case d < 24*time.Hour:
		hrs := int(d.Hours())
		if hrs == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hrs)
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "Yesterday"
		}
		return fmt.Sprintf("%d days ago", days)
	}
}

func deepMerge(dst, src map[string]any) {
	for k, v := range src {
		srcMap, srcOk := v.(map[string]any)
		dstMap, dstOk := dst[k].(map[string]any)
		if srcOk && dstOk {
			deepMerge(dstMap, srcMap)
		} else {
			dst[k] = v
		}
	}
}

func respondPortalError(w http.ResponseWriter, r *http.Request, httpStatus int, code, msg string) {
	respondError(w, r, httpStatus, code, msg)
}


