//intenza:paradigm imperative-shell

// Package app holds the use cases. Each one opens a transaction, loads
// aggregates, asks the functional core what should happen, saves the new
// state together with the events it produced (transactional outbox) and only
// then does the best-effort side effects, such as scheduling deadlines.
//
// Two clocks exist on purpose. The local Clock is for display; the store's
// Now is the database clock, the only authority for deadlines that unlock
// sealed prices or refuse late proposals.
package app

import (
	"context"
	"errors"
	"time"

	"github.com/flaviotinococoutinho/intenza/internal/domain/agreement"
	"github.com/flaviotinococoutinho/intenza/internal/domain/event"
	"github.com/flaviotinococoutinho/intenza/internal/domain/intent"
	"github.com/flaviotinococoutinho/intenza/internal/domain/proposal"
	"github.com/flaviotinococoutinho/intenza/internal/domain/reputation"
	"github.com/flaviotinococoutinho/intenza/internal/domain/taxonomy"
)

var (
	ErrNotFound    = errors.New("app: not found")
	ErrConflict    = errors.New("app: concurrent update, try again")
	ErrDuplicate   = errors.New("app: already exists")
	ErrForbidden   = errors.New("app: not allowed")
	ErrUnavailable = errors.New("app: dependency unavailable")
)

// Guard is a condition the store checks with the database clock while saving
// an intent, inside the same update as the version check.
type Guard int

const (
	GuardNone           Guard = iota
	GuardOpenBeforeEnd        // status open and closes_at > $$NOW
	GuardDeadlinePassed       // closes_at <= $$NOW
	GuardSelectionOver        // select_by <= $$NOW
)

// Party is a registered person or company with its credentials.
type Party struct {
	ID          string                `json:"id"`
	Name        string                `json:"name"`
	Document    string                `json:"document"`
	Kind        string                `json:"kind"` // person | company
	Credentials []taxonomy.Credential `json:"credentials"`
	Arbiter     bool                  `json:"arbiter"`
	CreatedAt   time.Time             `json:"created_at"`
}

// Envelope is a domain event with the identity the outbox gives it.
type Envelope struct {
	ID          string
	TraceParent string
	event.Event
}

// Tx is everything a use case may touch inside one transaction.
type Tx interface {
	Now(ctx context.Context) (time.Time, error)

	Intent(ctx context.Context, id string) (intent.Intent, error)
	InsertIntent(ctx context.Context, in intent.Intent) error
	SaveIntent(ctx context.Context, in intent.Intent, prevVersion int64, g Guard) error

	Proposal(ctx context.Context, id string) (proposal.Proposal, error)
	Proposals(ctx context.Context, intentID string) ([]proposal.Proposal, error)
	InsertProposal(ctx context.Context, p proposal.Proposal) error
	SaveProposal(ctx context.Context, p proposal.Proposal, prevVersion int64) error

	Agreement(ctx context.Context, id string) (agreement.Agreement, error)
	InsertAgreement(ctx context.Context, a agreement.Agreement) error
	SaveAgreement(ctx context.Context, a agreement.Agreement, prevVersion int64) error

	Party(ctx context.Context, id string) (Party, error)
	InsertParty(ctx context.Context, p Party) error

	RatingPair(ctx context.Context, agreementID string) (reputation.Pair, error)
	SaveRatingPair(ctx context.Context, p reputation.Pair) error
	InsertRatings(ctx context.Context, rs []reputation.Rating) error
	Ratings(ctx context.Context, rateeID string, role reputation.Role) ([]reputation.Rating, error)

	Outbox(ctx context.Context, events []Envelope) error
}

// Store runs transactions and the few queries that span aggregates.
type Store interface {
	Tx(ctx context.Context, fn func(ctx context.Context, tx Tx) error) error
	View(ctx context.Context, fn func(ctx context.Context, tx Tx) error) error
	OpenIntents(ctx context.Context, categoryID string, limit int) ([]intent.Intent, error)
	DueIntents(ctx context.Context, limit int) (closing, expiring []string, err error)
	DueAgreements(ctx context.Context, limit int) (ids []string, err error)
}

// Scheduler keeps a fast index of deadlines (Redis). It is an optimization:
// the store's due queries are the source of truth when it fails.
type Scheduler interface {
	Schedule(ctx context.Context, kind, id string, at time.Time) error
}

// IDs mints identifiers.
type IDs interface {
	New() string
	Derived(namespace, key string) string
}

// Clock is the local clock, for display only.
type Clock interface {
	Now() time.Time
}

// EscrowStatus is the escrow agent's view of an escrow.
type EscrowStatus string

const (
	EscrowPending  EscrowStatus = "pending"
	EscrowHeld     EscrowStatus = "held"
	EscrowReleased EscrowStatus = "released"
	EscrowRefunded EscrowStatus = "refunded"
)

// EscrowRequest opens an escrow for an agreement.
type EscrowRequest struct {
	Reference      string
	IdempotencyKey string
	AmountMinor    int64
	Currency       string
	MCC            string
	Payer, Payee   string
}

// EscrowRef is what the agent answers: its id and the Pix payment code.
type EscrowRef struct {
	ID          string
	PaymentCode string
}

// Escrow is the port to the authorized payment institution. It has no
// operation that moves money to the platform: the platform can only open an
// escrow and command its release to the seller or its refund to the buyer.
type Escrow interface {
	Open(ctx context.Context, r EscrowRequest) (EscrowRef, error)
	Release(ctx context.Context, escrowID, idempotencyKey string) error
	Refund(ctx context.Context, escrowID, idempotencyKey string) error
	Status(ctx context.Context, escrowID string) (EscrowStatus, error)
}

// Notification tells a seller about an intent it may answer.
type Notification struct {
	PartyID   string    `json:"party_id"`
	IntentID  string    `json:"intent_id"`
	CreatedAt time.Time `json:"created_at"`
}

// Inbox applies a consumer's effect at most once per event.
type Inbox interface {
	Once(ctx context.Context, consumer, eventID string, fn func(ctx context.Context, tx Tx) error) (bool, error)
}

// Backoffice holds the queries the workers need.
type Backoffice interface {
	PendingSettlements(ctx context.Context, olderThan time.Duration, limit int) ([]agreement.Agreement, error)
	FormedWithoutEscrow(ctx context.Context, olderThan time.Duration, limit int) ([]agreement.Agreement, error)
	EligibleSellers(ctx context.Context, required []string, exclude string, limit int) ([]Party, error)
	Notify(ctx context.Context, intentID string, partyIDs []string) error
	Notifications(ctx context.Context, partyID string, limit int) ([]Notification, error)
}
