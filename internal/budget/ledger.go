// Package budget enforces per-key spend limits by reserving worst-case cost before a request
// is forwarded and settling to actual usage afterwards.
package budget

import (
	"errors"
	"math"
	"sync"

	"github.com/sathwikbairaboina2/tollgate/internal/config"
)

// ErrExceeded means the reservation would take the key over its budget.
var ErrExceeded = errors.New("budget exceeded")

// Cost prices a request in USD. Prices are per one million tokens.
func Cost(p config.Price, inputTokens, outputTokens int64) float64 {
	return (float64(inputTokens)*p.Input + float64(outputTokens)*p.Output) / 1e6
}

// Usage is a snapshot of a ledger. Zero limits mean unlimited.
type Usage struct {
	LimitTokens, SpentTokens, HeldTokens int64
	LimitUSD, SpentUSD, HeldUSD          float64
}

// Ledger tracks spend and in-flight reservations for one key. Safe for concurrent use.
type Ledger struct {
	mu sync.Mutex
	u  Usage
}

// NewLedger returns an empty ledger with the given limits.
func NewLedger(b config.Budget) *Ledger {
	return &Ledger{u: Usage{LimitTokens: b.Tokens, LimitUSD: b.USD}}
}

// Reserve holds tokens and usd against the budget, or returns ErrExceeded.
func (l *Ledger) Reserve(tokens int64, usd float64) (*Reservation, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.u.LimitTokens > 0 && l.u.SpentTokens+l.u.HeldTokens+tokens > l.u.LimitTokens {
		return nil, ErrExceeded
	}
	if l.u.LimitUSD > 0 && l.u.SpentUSD+l.u.HeldUSD+usd > l.u.LimitUSD {
		return nil, ErrExceeded
	}
	l.u.HeldTokens += tokens
	l.u.HeldUSD += usd
	return &Reservation{l: l, tokens: tokens, usd: usd}, nil
}

// Usage returns a snapshot of the ledger.
func (l *Ledger) Usage() Usage {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.u
}

// Reservation is an in-flight hold on a ledger.
type Reservation struct {
	l      *Ledger
	tokens int64
	usd    float64
	done   bool
}

// Settle releases the hold and records actual spend. Only the first Settle or Release counts.
func (r *Reservation) Settle(tokens int64, usd float64) { r.finish(tokens, usd) }

// Release drops the hold without recording spend.
func (r *Reservation) Release() { r.finish(0, 0) }

func (r *Reservation) finish(tokens int64, usd float64) {
	l := r.l
	l.mu.Lock()
	defer l.mu.Unlock()
	if r.done {
		return
	}
	r.done = true
	l.u.HeldTokens -= r.tokens
	l.u.HeldUSD -= r.usd
	if math.Abs(l.u.HeldUSD) < 1e-12 {
		l.u.HeldUSD = 0 // drop float residue so "nothing held" reads as exactly zero
	}
	l.u.SpentTokens += tokens
	l.u.SpentUSD += usd
}
