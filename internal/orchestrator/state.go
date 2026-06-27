package orchestrator

import "zia/internal/domain"

var transitions = map[domain.PaymentIntentStatus][]domain.PaymentIntentStatus{
	domain.PICreated:        {domain.PIRequiresAction, domain.PIProcessing, domain.PIFailed, domain.PIExpired},
	domain.PIRequiresAction: {domain.PIProcessing, domain.PIFailed, domain.PIExpired},
	domain.PIProcessing:     {domain.PISucceeded, domain.PIFailed, domain.PIRequiresAction},
	domain.PISucceeded:      {domain.PIPartiallyRefunded, domain.PIRefunded},
}

func IsValidTransition(from, to domain.PaymentIntentStatus) bool {
	for _, allowed := range transitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}
