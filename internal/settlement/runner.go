package settlement

import (
	"context"
	"fmt"
	"time"

	"zia/internal/connector"
	"zia/internal/domain"
	"zia/internal/ledger"
	"zia/internal/repository"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type Runner struct {
	merchantRepo repository.MerchantRepository
	payoutRepo   repository.PayoutRepository
	ledgerEng    *ledger.Engine
	connRegistry *connector.Registry
	logger       *zap.Logger
}

func NewRunner(
	merchantRepo repository.MerchantRepository,
	payoutRepo repository.PayoutRepository,
	ledgerEng *ledger.Engine,
	connRegistry *connector.Registry,
	logger *zap.Logger,
) *Runner {
	return &Runner{
		merchantRepo: merchantRepo,
		payoutRepo:   payoutRepo,
		ledgerEng:    ledgerEng,
		connRegistry: connRegistry,
		logger:       logger,
	}
}

type SettlementPolicy struct {
	MinBalanceMinor int64
	MaxPayoutMinor  int64
}

var DefaultPolicy = SettlementPolicy{
	MinBalanceMinor: 500_00,
	MaxPayoutMinor:  10_000_00,
}

func (r *Runner) Settle(ctx context.Context, policy SettlementPolicy) error {
	r.logger.Info("settlement run starting",
		zap.Int64("min_balance", policy.MinBalanceMinor),
		zap.Int64("max_payout", policy.MaxPayoutMinor))

	merchants, err := r.merchantRepo.ListAll(ctx)
	if err != nil {
		return fmt.Errorf("list merchants: %w", err)
	}

	r.logger.Info("settlement: loaded merchants", zap.Int("count", len(merchants)))

	var settled, skipped int
	for _, m := range merchants {
		if err := r.settleMerchant(ctx, m, policy); err != nil {
			r.logger.Error("settlement failed for merchant",
				zap.String("merchant_id", m.ID),
				zap.Error(err))
			continue
		}
		settled++
	}

	r.logger.Info("settlement run completed",
		zap.Int("settled", settled),
		zap.Int("skipped", skipped))
	return nil
}

func (r *Runner) settleMerchant(ctx context.Context, m domain.Merchant, policy SettlementPolicy) error {
	availableAcct := ledger.MerchantAvailable(m.ID)
	balance, err := r.ledgerEng.Balance(ctx, availableAcct)
	if err != nil {
		return fmt.Errorf("get balance for %s: %w", availableAcct, err)
	}

	if balance < policy.MinBalanceMinor {
		r.logger.Debug("merchant balance below threshold, skipping",
			zap.String("merchant_id", m.ID),
			zap.Int64("balance", balance),
			zap.Int64("threshold", policy.MinBalanceMinor))
		return nil
	}

	payoutAmount := balance
	if payoutAmount > policy.MaxPayoutMinor {
		payoutAmount = policy.MaxPayoutMinor
	}

	rail := r.selectRail(m)

	conn, ok := r.connRegistry.Get(rail)
	if !ok {
		return fmt.Errorf("no connector registered for rail %s", rail)
	}

	payoutID := uuid.New().String()

	colReq := connector.PayoutRequest{
		MerchantID:     m.ID,
		AmountMinor:    payoutAmount,
		Currency:       m.DefaultCurrency,
		TargetCurrency: m.DefaultCurrency,
		BankAccountRef: m.ID,
		IdempotencyKey: payoutID,
	}

	result, err := conn.InitiatePayout(ctx, colReq)
	if err != nil {
		r.logger.Error("settlement payout failed on initiate",
			zap.String("merchant_id", m.ID),
			zap.String("rail", rail),
			zap.Error(err))
		return err
	}

	payout := &domain.Payout{
		ID:          payoutID,
		MerchantID:  m.ID,
		AmountMinor: payoutAmount,
		Currency:    m.DefaultCurrency,
		Rail:        rail,
		Status:      domain.PayoutPending,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}

	if result.PSPReference != "" {
		payout.PSPReference = &result.PSPReference
	}

	if err := r.payoutRepo.Create(ctx, payout); err != nil {
		return fmt.Errorf("save payout: %w", err)
	}

	if err := r.ledgerEng.PostPayoutInit(ctx, m.ID, payoutID, payoutAmount, m.DefaultCurrency); err != nil {
		r.logger.Error("ledger post for payout init failed",
			zap.String("payout_id", payoutID),
			zap.Error(err))
	}

	if result.Status == "succeeded" {
		if err := r.payoutRepo.UpdateStatus(ctx, payoutID, domain.PayoutSucceeded); err != nil {
			return fmt.Errorf("update payout status: %w", err)
		}
		if err := r.ledgerEng.PostPayoutComplete(ctx, m.ID, payoutID, payoutAmount, m.DefaultCurrency); err != nil {
			r.logger.Error("ledger post for payout complete failed",
				zap.String("payout_id", payoutID),
				zap.Error(err))
		}
	}

	r.logger.Info("settlement payout created",
		zap.String("merchant_id", m.ID),
		zap.String("payout_id", payoutID),
		zap.Int64("amount", payoutAmount),
		zap.String("rail", rail),
		zap.String("status", result.Status))

	return nil
}

func (r *Runner) selectRail(m domain.Merchant) string {
	currency := m.DefaultCurrency
	for _, name := range r.connRegistry.Names() {
		conn, ok := r.connRegistry.Get(name)
		if !ok {
			continue
		}
		caps := conn.Capabilities()
		if !caps.SupportsPayout {
			continue
		}
		for _, c := range caps.SupportedCurrencies {
			if c == currency {
				return name
			}
		}
	}
	return "pesalink"
}
