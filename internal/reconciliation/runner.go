package reconciliation

import (
	"context"
	"fmt"
	"time"

	"zia/internal/connector"
	"zia/internal/repository"

	"go.uber.org/zap"
)

type Runner struct {
	connRegistry *connector.Registry
	attRepo      repository.AttemptRepository
	logger       *zap.Logger
}

func NewRunner(
	connRegistry *connector.Registry,
	attRepo repository.AttemptRepository,
	logger *zap.Logger,
) *Runner {
	return &Runner{
		connRegistry: connRegistry,
		attRepo:      attRepo,
		logger:       logger,
	}
}

func (r *Runner) Reconcile(ctx context.Context, date time.Time) error {
	r.logger.Info("starting reconciliation run",
		zap.String("date", date.Format("2006-01-02")))

	startOfDay := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
	endOfDay := startOfDay.Add(24 * time.Hour)

	attempts, err := r.attRepo.ListByDateRange(ctx, startOfDay, endOfDay)
	if err != nil {
		return fmt.Errorf("list attempts by date: %w", err)
	}

	r.logger.Info("reconciliation: loaded local attempts",
		zap.Int("count", len(attempts)))

	for _, name := range r.connRegistry.Names() {
		conn, ok := r.connRegistry.Get(name)
		if !ok {
			continue
		}
		r.reconcilePSP(ctx, name, conn, attempts)
	}

	return nil
}

func (r *Runner) reconcilePSP(ctx context.Context, name string, conn connector.Connector, localAttempts []repository.AttemptRow) {
	r.logger.Info("reconciling PSP", zap.String("psp", name))

	var matched, unmatched, errors int
	for _, att := range localAttempts {
		if att.PSP != name {
			continue
		}

		if att.PSPReference == "" {
			unmatched++
			continue
		}

		if !conn.Capabilities().SupportsCollection && !conn.Capabilities().SupportsPayout {
			matched++
			continue
		}

		statusResult, err := conn.GetStatus(ctx, att.PSPReference)
		if err != nil {
			r.logger.Warn("reconciliation: GetStatus failed",
				zap.String("psp", name),
				zap.String("psp_reference", att.PSPReference),
				zap.Error(err))
			errors++
			continue
		}

		if !statusResult.Supported {
			matched++
			continue
		}

		localStatus := string(att.Status)
		pspStatus := statusResult.Status

		if localStatus == "succeeded" && pspStatus != "succeeded" {
			r.logger.Warn("reconciliation MISMATCH: local=success psp!=success",
				zap.String("psp", name),
				zap.String("psp_reference", att.PSPReference),
				zap.String("local_status", localStatus),
				zap.String("psp_status", pspStatus))
			unmatched++
			continue
		}

		if statusResult.AmountMinor > 0 && statusResult.AmountMinor != att.AmountMinor {
			r.logger.Warn("reconciliation MISMATCH: amount differs",
				zap.String("psp", name),
				zap.String("psp_reference", att.PSPReference),
				zap.Int64("local_amount", att.AmountMinor),
				zap.Int64("psp_amount", statusResult.AmountMinor))
			unmatched++
			continue
		}

		matched++
	}

	r.logger.Info("reconciliation result",
		zap.String("psp", name),
		zap.Int("matched", matched),
		zap.Int("unmatched", unmatched),
		zap.Int("errors", errors))
}
