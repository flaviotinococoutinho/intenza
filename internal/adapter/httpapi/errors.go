package httpapi

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"

	"github.com/flaviotinococoutinho/intenza/internal/app"
	"github.com/flaviotinococoutinho/intenza/internal/domain/agreement"
	"github.com/flaviotinococoutinho/intenza/internal/domain/fsm"
	"github.com/flaviotinococoutinho/intenza/internal/domain/intent"
	"github.com/flaviotinococoutinho/intenza/internal/domain/money"
	"github.com/flaviotinococoutinho/intenza/internal/domain/proposal"
	"github.com/flaviotinococoutinho/intenza/internal/domain/reputation"
	"github.com/flaviotinococoutinho/intenza/internal/domain/schema"
	"github.com/flaviotinococoutinho/intenza/internal/domain/selection"
	"github.com/flaviotinococoutinho/intenza/internal/domain/taxonomy"
)

func anyIs(err error, targets ...error) bool {
	for _, t := range targets {
		if errors.Is(err, t) {
			return true
		}
	}
	return false
}

// problem maps use-case errors to RFC 9457 responses.
func (s *server) problem(ctx context.Context, err error) error {
	var se huma.StatusError
	if errors.As(err, &se) {
		return err
	}
	var fields schema.Errors
	if errors.As(err, &fields) {
		details := make([]error, len(fields))
		for i, f := range fields {
			details[i] = &huma.ErrorDetail{Message: f.Reason, Location: "body." + f.Field}
		}
		return huma.Error422UnprocessableEntity("atributos inválidos para o schema da categoria", details...)
	}
	switch {
	case anyIs(err, app.ErrNotFound, taxonomy.ErrUnknownCategory):
		return huma.Error404NotFound(err.Error())
	case anyIs(err, app.ErrForbidden, proposal.ErrNotOwner, intent.ErrNotBuyer, agreement.ErrNotParty,
		proposal.ErrIneligible, proposal.ErrSelfDealing, proposal.ErrSealedNow, reputation.ErrNotPair):
		return huma.Error403Forbidden(err.Error())
	case anyIs(err, app.ErrConflict, app.ErrDuplicate, fsm.ErrTransition, intent.ErrNotOpen, intent.ErrTooEarly,
		intent.ErrTooLate, agreement.ErrTooEarly, agreement.ErrTooLate, agreement.ErrNoDiscrepancy, agreement.ErrConflict,
		agreement.ErrNotAwarded, reputation.ErrTwice, reputation.ErrWindow, proposal.ErrSchema):
		return huma.Error409Conflict(err.Error())
	case anyIs(err, schema.ErrInvalid, taxonomy.ErrNotLeaf, taxonomy.ErrNotLaunched, intent.ErrWindow, intent.ErrTitle,
		intent.ErrDelivery, proposal.ErrPrice, proposal.ErrLeadTime, proposal.ErrValidity, money.ErrFormat, money.ErrCurrency,
		selection.ErrWeights, agreement.ErrTracking, reputation.ErrStars):
		return huma.Error422UnprocessableEntity(err.Error())
	case anyIs(err, app.ErrUnavailable, context.DeadlineExceeded):
		return &retryError{huma.Error503ServiceUnavailable("dependência indisponível, tente de novo"), "2"}
	}
	s.d.Log.ErrorContext(ctx, "erro não mapeado", "err", err)
	return huma.Error500InternalServerError("erro interno")
}
