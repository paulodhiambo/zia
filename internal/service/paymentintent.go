package service

import (
	"context"

	"zia/internal/domain"
	"zia/internal/orchestrator"
)

type PaymentIntentService struct {
	orc *orchestrator.Engine
}

func NewPaymentIntent(orc *orchestrator.Engine) *PaymentIntentService {
	return &PaymentIntentService{orc: orc}
}

func (s *PaymentIntentService) Create(ctx context.Context, req orchestrator.CreatePIRequest) (*orchestrator.CreatePIResult, error) {
	return s.orc.CreatePaymentIntent(ctx, req)
}

func (s *PaymentIntentService) GetByID(ctx context.Context, id string) (*domain.PaymentIntent, error) {
	return s.orc.GetPaymentIntent(ctx, id)
}
