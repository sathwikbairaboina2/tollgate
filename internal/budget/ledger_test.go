package budget

import (
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/sathwikbairaboina2/tollgate/internal/config"
)

func TestCost(t *testing.T) {
	got := Cost(config.Price{Input: 0.15, Output: 0.60}, 1_000_000, 500_000)
	if math.Abs(got-0.45) > 1e-12 {
		t.Fatalf("Cost = %v, want 0.45", got)
	}
}

func TestLedger_UnlimitedAlwaysReserves(t *testing.T) {
	l := NewLedger(config.Budget{})
	if _, err := l.Reserve(1<<40, 1e9); err != nil {
		t.Fatalf("unlimited ledger rejected: %v", err)
	}
}

func TestLedger_TokenLimitBoundary(t *testing.T) {
	l := NewLedger(config.Budget{Tokens: 100})
	r, err := l.Reserve(100, 0)
	if err != nil {
		t.Fatalf("reservation equal to limit rejected: %v", err)
	}
	r.Release()
	if _, err := l.Reserve(101, 0); !errors.Is(err, ErrExceeded) {
		t.Fatalf("reservation over limit: err = %v, want ErrExceeded", err)
	}
}

func TestLedger_USDLimit(t *testing.T) {
	l := NewLedger(config.Budget{USD: 1.0})
	if _, err := l.Reserve(0, 1.01); !errors.Is(err, ErrExceeded) {
		t.Fatalf("err = %v, want ErrExceeded", err)
	}
	if _, err := l.Reserve(0, 1.0); err != nil {
		t.Fatalf("reservation equal to USD limit rejected: %v", err)
	}
}

func TestLedger_HeldReservationsCountAgainstLimit(t *testing.T) {
	l := NewLedger(config.Budget{Tokens: 100})
	if _, err := l.Reserve(60, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Reserve(60, 0); !errors.Is(err, ErrExceeded) {
		t.Fatalf("second reservation should not fit while first is held, err = %v", err)
	}
}

func TestLedger_SettleReplacesHoldWithActual(t *testing.T) {
	l := NewLedger(config.Budget{Tokens: 100, USD: 10})
	r, _ := l.Reserve(80, 8)
	r.Settle(30, 3)
	u := l.Usage()
	if u.HeldTokens != 0 || u.HeldUSD != 0 || u.SpentTokens != 30 || math.Abs(u.SpentUSD-3) > 1e-12 {
		t.Fatalf("usage after settle = %+v", u)
	}
	r.Settle(30, 3) // idempotent
	if l.Usage().SpentTokens != 30 {
		t.Fatal("double settle charged twice")
	}
}

func TestLedger_ReleaseChargesNothing(t *testing.T) {
	l := NewLedger(config.Budget{Tokens: 100})
	r, _ := l.Reserve(80, 0)
	r.Release()
	if u := l.Usage(); u.HeldTokens != 0 || u.SpentTokens != 0 {
		t.Fatalf("usage after release = %+v", u)
	}
}

func TestLedger_ActualAboveReservationIsRecordedAndBlocks(t *testing.T) {
	l := NewLedger(config.Budget{Tokens: 100})
	r, _ := l.Reserve(10, 0)
	r.Settle(150, 0)
	if l.Usage().SpentTokens != 150 {
		t.Fatalf("spent = %d, want actual 150", l.Usage().SpentTokens)
	}
	if _, err := l.Reserve(1, 0); !errors.Is(err, ErrExceeded) {
		t.Fatalf("overspent ledger still reserving, err = %v", err)
	}
}

func TestLedger_ConcurrentReservationsNeverExceedLimit(t *testing.T) {
	l := NewLedger(config.Budget{Tokens: 250})
	var ok atomic.Int64
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := l.Reserve(10, 0); err == nil {
				ok.Add(1)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 25 {
		t.Fatalf("admitted %d reservations of 10 against 250, want 25", ok.Load())
	}
}
