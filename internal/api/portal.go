package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
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
	notifRepo       repository.NotificationRepository
	logger          *zap.Logger
}

type portalCtxKey string

const portalUserID portalCtxKey = "portal_user_id"

func PortalAuth(repo repository.SessionRepository) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
				writePortalUnauthorized(w)
				return
			}
			token := strings.TrimPrefix(auth, "Bearer ")
			session, err := repo.GetByToken(r.Context(), token)
			if err != nil {
				writePortalUnauthorized(w)
				return
			}
			ctx := context.WithValue(r.Context(), portalUserID, session.UserID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func writePortalUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(portalError("1002", "Authentication failed"))
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
	notifRepo repository.NotificationRepository,
	logger *zap.Logger,
) *PortalHandler {
	return &PortalHandler{
		userRepo: userRepo,
		sessionRepo: sessionRepo,
		merchantRepo: merchantRepo,
		piRepo: piRepo,
		payoutRepo: payoutRepo,
		ledgerRepo: ledgerRepo,
		customerRepo: customerRepo,
		teamMemberRepo: teamMemberRepo,
		teamInviteRepo: teamInviteRepo,
		webhookEPRepo: webhookEPRepo,
		notifRepo: notifRepo,
		logger: logger,
	}
}

func (h *PortalHandler) getUserID(r *http.Request) string {
	if id, ok := r.Context().Value(portalUserID).(string); ok {
		return id
	}
	return ""
}

func (h *PortalHandler) getMerchantID(r *http.Request) string {
	userID := h.getUserID(r)
	if userID == "" {
		return ""
	}
	user, err := h.userRepo.GetByID(r.Context(), userID)
	if err != nil {
		return ""
	}
	return user.MerchantID
}

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
	if err := json.Unmarshal(env.PrimaryData, &creds); err != nil {
		respondPortalError(w, r, http.StatusBadRequest, "1001", "Invalid credentials format")
		return
	}

	merchantID := "mch_default"
	user, err := h.userRepo.GetByEmail(r.Context(), merchantID, creds.Email)
	if err != nil {
		respondPortalError(w, r, http.StatusUnauthorized, "1002", "Invalid email or password")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(creds.Password)); err != nil {
		respondPortalError(w, r, http.StatusUnauthorized, "1002", "Invalid email or password")
		return
	}

	token := uuid.New().String()
	session := &domain.Session{
		ID:        uuid.New().String(),
		UserID:    user.ID,
		Token:     token,
		ExpiresAt: time.Now().Add(24 * time.Hour),
		CreatedAt: time.Now(),
	}
	if err := h.sessionRepo.Create(r.Context(), session); err != nil {
		h.logger.Error("session create", zap.Error(err))
		respondPortalError(w, r, http.StatusInternalServerError, "1007", "Internal error")
		return
	}

	respond(w, r, http.StatusOK, map[string]any{
		"token": token,
		"user": map[string]string{
			"name":  user.Name,
			"email": user.Email,
			"role":  user.Role,
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
		Password string `json:"password"`
	}
	if err := json.Unmarshal(env.PrimaryData, &data); err != nil {
		respondPortalError(w, r, http.StatusBadRequest, "1001", "Invalid signup data")
		return
	}

	merchantID := uuid.New().String()
	if err := h.merchantRepo.Create(r.Context(), &domain.Merchant{
		ID:              merchantID,
		LegalName:       data.Company,
		Country:         "US",
		DefaultCurrency: "USD",
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
	respond(w, r, http.StatusOK, map[string]bool{"success": true})
}

func (h *PortalHandler) DashboardOverview(w http.ResponseWriter, r *http.Request) {
	merchantID := h.getMerchantID(r)

	pis, err := h.piRepo.ListByMerchant(r.Context(), merchantID, 100, 0)
	if err != nil {
		h.logger.Error("list transactions for dashboard", zap.Error(err))
	}

	var todayVolume int64
	var successfulPayments int
	for _, pi := range pis {
		if pi.CreatedAt.After(time.Now().Truncate(24 * time.Hour)) {
			todayVolume += pi.AmountMinor
		}
		if pi.Status == domain.PISucceeded {
			successfulPayments++
		}
	}

	balance, _ := h.ledgerRepo.Balance(r.Context(), ledger.MerchantAvailable(merchantID))
	floatBalance := float64(balance) / 100.0
	floatToday := float64(todayVolume) / 100.0

	payoutsList, _ := h.payoutRepo.ListByMerchant(r.Context(), merchantID, 100, 0)
	var pendingPayoutsMinor int64
	for _, p := range payoutsList {
		if p.Status == domain.PayoutPending {
			pendingPayoutsMinor += p.AmountMinor
		}
	}
	floatPending := float64(pendingPayoutsMinor) / 100.0

	respond(w, r, http.StatusOK, map[string]any{
		"treasuryBalance":    floatBalance,
		"todayVolume":        floatToday,
		"successfulPayments": successfulPayments,
		"pendingPayouts":     floatPending,
		"checklist": []map[string]any{
			{"task": "Verify business entity", "status": "Done", "completed": true},
			{"task": "Connect payment method", "status": "Done", "completed": true},
			{"task": "Invite team members", "status": "Pending", "completed": false},
			{"task": "Set up webhook", "status": "Pending", "completed": false},
			{"task": "API integration live", "status": "Pending", "completed": false},
		},
	})
}

func (h *PortalHandler) ListTransactions(w http.ResponseWriter, r *http.Request) {
	merchantID := h.getMerchantID(r)

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 10
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
		amount := float64(pi.AmountMinor) / 100.0
		status := mapStatus(pi.Status)
		txs = append(txs, map[string]any{
			"id":           pi.ID,
			"date":         pi.CreatedAt.Format("Jan 2 · 15:04"),
			"counterparty": pi.CustomerRef,
			"method":       string(pi.Method),
			"amount":       fmt.Sprintf("+$%.2f", amount),
			"status":       status,
		})
	}

	respond(w, r, http.StatusOK, map[string]any{
		"transactions": txs,
	})
}

func (h *PortalHandler) ListPayouts(w http.ResponseWriter, r *http.Request) {
	merchantID := h.getMerchantID(r)

	payouts, err := h.payoutRepo.ListByMerchant(r.Context(), merchantID, 100, 0)
	if err != nil {
		h.logger.Error("list payouts", zap.Error(err))
		respondPortalError(w, r, http.StatusInternalServerError, "1007", "Internal error")
		return
	}

	resp := make([]map[string]any, 0, len(payouts))
	for _, p := range payouts {
		amount := float64(p.AmountMinor) / 100.0
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
			"amount":      fmt.Sprintf("$%.2f", amount),
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
		Description string  `json:"description"`
	}
	if err := json.Unmarshal(env.PrimaryData, &data); err != nil {
		respondPortalError(w, r, http.StatusBadRequest, "1001", "Invalid payout data")
		return
	}

	amountMinor := int64(data.Amount * 100)
	payout := &domain.Payout{
		ID:          uuid.New().String(),
		MerchantID:  merchantID,
		AmountMinor: amountMinor,
		Currency:    "USD",
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
		"destination": data.Bank + " •••• " + data.Account[len(data.Account)-4:],
		"bank":        data.Bank,
		"amount":      fmt.Sprintf("$%.2f", data.Amount),
		"status":      "Pending",
	})
}

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
		volume := float64(c.VolumeMinor) / 100.0
		ltv := float64(c.LTVMinor) / 100.0
		resp = append(resp, map[string]any{
			"id":            c.ID,
			"name":          c.Name,
			"company":       c.Company,
			"email":         c.Email,
			"phone":         c.Phone,
			"location":      c.Location,
			"volume":        fmt.Sprintf("$%.2f", volume),
			"ltv":           fmt.Sprintf("$%.2f", ltv),
			"joined":        c.JoinedAt.Format("Jan 2, 2006"),
			"status":        c.Status,
			"paymentMethod": c.PaymentMethod,
		})
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

	volume := float64(c.VolumeMinor) / 100.0
	ltv := float64(c.LTVMinor) / 100.0
	respond(w, r, http.StatusOK, map[string]any{
		"id":            c.ID,
		"name":          c.Name,
		"company":       c.Company,
		"email":         c.Email,
		"phone":         c.Phone,
		"location":      c.Location,
		"volume":        fmt.Sprintf("$%.2f", volume),
		"ltv":           fmt.Sprintf("$%.2f", ltv),
		"joined":        c.JoinedAt.Format("Jan 2, 2006"),
		"status":        c.Status,
		"paymentMethod": c.PaymentMethod,
	})
}

func (h *PortalHandler) GetCustomer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	c, err := h.customerRepo.GetByID(r.Context(), id)
	if err != nil {
		respondPortalError(w, r, http.StatusNotFound, "1003", "Customer not found")
		return
	}
	volume := float64(c.VolumeMinor) / 100.0
	ltv := float64(c.LTVMinor) / 100.0
	respond(w, r, http.StatusOK, map[string]any{
		"id":            c.ID,
		"name":          c.Name,
		"company":       c.Company,
		"email":         c.Email,
		"phone":         c.Phone,
		"location":      c.Location,
		"volume":        fmt.Sprintf("$%.2f", volume),
		"ltv":           fmt.Sprintf("$%.2f", ltv),
		"joined":        c.JoinedAt.Format("Jan 2, 2006"),
		"status":        c.Status,
		"paymentMethod": c.PaymentMethod,
	})
}

func (h *PortalHandler) GetCustomerCharges(w http.ResponseWriter, r *http.Request) {
	respond(w, r, http.StatusOK, map[string]any{"charges": []any{}})
}

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
			"email":  inv.Email,
			"role":   inv.Role,
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
	if err := json.Unmarshal(env.PrimaryData, &data); err != nil {
		respondPortalError(w, r, http.StatusBadRequest, "1001", "Invalid invite data")
		return
	}

	existing, _ := h.teamMemberRepo.GetByEmail(r.Context(), merchantID, data.Email)
	if existing != nil {
		respondPortalError(w, r, http.StatusBadRequest, "1001", "User is already a team member")
		return
	}
	existingInv, _ := h.teamInviteRepo.GetByEmail(r.Context(), merchantID, data.Email)
	if existingInv != nil {
		respondPortalError(w, r, http.StatusBadRequest, "1001", "Invitation already sent")
		return
	}

	token := uuid.New().String()
	inv := &domain.TeamInvitation{
		ID:         uuid.New().String(),
		MerchantID: merchantID,
		Email:      data.Email,
		Role:       data.Role,
		Token:      token,
		ExpiresAt:  time.Now().Add(7 * 24 * time.Hour),
		CreatedAt:  time.Now(),
	}
	if err := h.teamInviteRepo.Create(r.Context(), inv); err != nil {
		h.logger.Error("create invitation", zap.Error(err))
		respondPortalError(w, r, http.StatusInternalServerError, "1007", "Failed to send invitation")
		return
	}

	respond(w, r, http.StatusOK, map[string]any{
		"email":  inv.Email,
		"role":   inv.Role,
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
	inv.CreatedAt = time.Now()
	if err := h.teamInviteRepo.Create(r.Context(), inv); err != nil {
		h.logger.Error("resend invitation", zap.Error(err))
		respondPortalError(w, r, http.StatusInternalServerError, "1007", "Failed to resend")
		return
	}
	respond(w, r, http.StatusOK, map[string]bool{"success": true})
}

func (h *PortalHandler) RevokeInvitation(w http.ResponseWriter, r *http.Request) {
	merchantID := h.getMerchantID(r)
	email := r.PathValue("email")
	if err := h.teamInviteRepo.DeleteByEmail(r.Context(), merchantID, email); err != nil {
		h.logger.Error("revoke invitation", zap.Error(err))
		respondPortalError(w, r, http.StatusNotFound, "1003", "No invitation found")
		return
	}
	respond(w, r, http.StatusOK, map[string]bool{"success": true})
}

func (h *PortalHandler) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	respond(w, r, http.StatusOK, map[string]any{
		"keys": []map[string]string{
			{"name": "Production · Server", "key": "sk_live_••••_8Q21f3", "env": "live", "last": "12 min ago"},
			{"name": "Sandbox · Server", "key": "sk_test_••••_aB34cD", "env": "test", "last": "2 days ago"},
		},
	})
}

func (h *PortalHandler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
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
	if err := json.Unmarshal(env.PrimaryData, &data); err != nil {
		respondPortalError(w, r, http.StatusBadRequest, "1001", "Invalid key data")
		return
	}

	prefix := "sk"
	if data.Type == "Publishable" {
		prefix = "pk"
	}
	keyBytes := make([]byte, 24)
	rand.Read(keyBytes)
	key := fmt.Sprintf("%s_%s_%s", prefix, data.Env, hex.EncodeToString(keyBytes))
	masked := fmt.Sprintf("%s_%s_••••_%s", prefix, data.Env, hex.EncodeToString(keyBytes)[:6])

	respond(w, r, http.StatusOK, map[string]string{
		"name": data.Name,
		"key":  masked,
		"env":  data.Env,
		"last": "Just now",
	})
	_ = key
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
	if err := json.Unmarshal(env.PrimaryData, &data); err != nil {
		respondPortalError(w, r, http.StatusBadRequest, "1001", "Invalid webhook data")
		return
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
		"url":    ep.URL,
		"events": ep.Events,
		"status": ep.Status,
	})
}

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
	h.notifRepo.MarkAllRead(r.Context(), merchantID)
	respond(w, r, http.StatusOK, map[string]bool{"success": true})
}

func (h *PortalHandler) UpdateNotificationPreferences(w http.ResponseWriter, r *http.Request) {
	respond(w, r, http.StatusOK, map[string]bool{"success": true})
}

func (h *PortalHandler) GetProfile(w http.ResponseWriter, r *http.Request) {
	userID := h.getUserID(r)
	user, err := h.userRepo.GetByID(r.Context(), userID)
	if err != nil {
		respondPortalError(w, r, http.StatusNotFound, "1003", "User not found")
		return
	}

	respond(w, r, http.StatusOK, map[string]any{
		"user": map[string]string{
			"name":  user.Name,
			"title": user.Title,
			"email": user.Email,
			"phone": user.Phone,
			"role":  user.Role,
		},
		"security": map[string]any{
			"passwordLastChanged": "41 days ago",
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
		"integrations": []map[string]string{
			{"name": "Slack", "detail": "#finance-alerts"},
		},
	})
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

	user, _ := h.userRepo.GetByID(r.Context(), userID)
	respond(w, r, http.StatusOK, map[string]any{
		"user": map[string]string{
			"name":  user.Name,
			"title": user.Title,
			"email": user.Email,
			"phone": user.Phone,
			"role":  user.Role,
		},
		"security": map[string]any{
			"passwordLastChanged": "41 days ago",
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
		"integrations": []map[string]string{
			{"name": "Slack", "detail": "#finance-alerts"},
		},
	})
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
		h := int(d.Hours())
		if h == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", h)
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "Yesterday"
		}
		return fmt.Sprintf("%d days ago", days)
	}
}

func randInt(max int) int {
	n, _ := rand.Int(rand.Reader, big.NewInt(int64(max)))
	return int(n.Int64())
}

func respondPortalError(w http.ResponseWriter, r *http.Request, httpStatus int, code, msg string) {
	respondError(w, r, httpStatus, code, msg)
}

func portalError(code, msg string) any {
	return map[string]any{
		"statusCode":        code,
		"statusDescription": "BusinessError",
		"messageCode":       code[:3],
		"messageDescription": msg,
		"errorInfo":         map[string]string{"errorCode": code, "errorMessage": msg},
		"primaryData":       nil,
	}
}
