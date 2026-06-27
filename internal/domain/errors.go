package domain

import "fmt"

type ErrInsufficientBalance struct {
	AccountID string
}

func (e ErrInsufficientBalance) Error() string {
	return fmt.Sprintf("insufficient balance in account %s", e.AccountID)
}

type ErrInvalidStateTransition struct {
	From string
	To   string
}

func (e ErrInvalidStateTransition) Error() string {
	return fmt.Sprintf("invalid state transition from %s to %s", e.From, e.To)
}

type ErrConnectorNotAvailable struct {
	PSP string
}

func (e ErrConnectorNotAvailable) Error() string {
	return fmt.Sprintf("connector %s is not available", e.PSP)
}

type ErrIdempotencyConflict struct {
	Key string
}

func (e ErrIdempotencyConflict) Error() string {
	return fmt.Sprintf("idempotency key %s already used with a different payload", e.Key)
}

type ErrValidation struct {
	Field   string
	Message string
}

func (e ErrValidation) Error() string {
	return fmt.Sprintf("validation error on %s: %s", e.Field, e.Message)
}

type ErrNotFound struct {
	Resource string
	ID       string
}

func (e ErrNotFound) Error() string {
	return fmt.Sprintf("%s not found: %s", e.Resource, e.ID)
}
