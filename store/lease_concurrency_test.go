package store

import (
	"context"
	"errors"
	"sync"
	"testing"

	"potatoeye-cutseed-sprout-gate/catalog"
	"potatoeye-cutseed-sprout-gate/ledger"
	"potatoeye-cutseed-sprout-gate/task"
)

func TestDuplicateSealRejected(t *testing.T) {
	s := newMemStore(t)
	lockDefault(t, s)
	req := defaultLockRequest()
	req.Batch = "B-2"
	req.BasketSeals = []string{"seal-1", "seal-3"}
	req.BlindCodes = []string{"code-3", "code-4"}
	if _, err := s.Lock(context.Background(), req); err == nil {
		t.Fatal("duplicate basket seal must be rejected")
	} else {
		var se *Error
		if !errors.As(err, &se) || se.Code != CodeConflict {
			t.Fatalf("err = %v; want CONFLICT", err)
		}
	}
}

func TestDuplicateBlindCodeRejected(t *testing.T) {
	s := newMemStore(t)
	lockDefault(t, s)
	req := defaultLockRequest()
	req.Batch = "B-2"
	req.BasketSeals = []string{"seal-3", "seal-4"}
	req.BlindCodes = []string{"code-1", "code-5"}
	if _, err := s.Lock(context.Background(), req); err == nil {
		t.Fatal("duplicate blind code must be rejected")
	} else {
		var se *Error
		if !errors.As(err, &se) || se.Code != CodeConflict {
			t.Fatalf("err = %v; want CONFLICT", err)
		}
	}
}

// advanceToSealing locks a task with the given request and drives it through
// two cut confirmations so it sits in the sealing_samples state.
func advanceToSealing(t *testing.T, s *SQLite, req LockRequest) (task.ID, task.Generation) {
	t.Helper()
	ctx := context.Background()
	res, err := s.Lock(ctx, req)
	if err != nil {
		t.Fatalf("lock: %v", err)
	}
	id, gen := res.TaskID, res.Generation
	snap := currentSnapshot(t, s, id)
	for _, p := range []catalog.PersonnelID{"cutter-1", "cutter-2"} {
		if _, err := s.CutConfirm(ctx, id, CutConfirmRequest{PersonnelID: p, Generation: gen, Snapshot: snap}); err != nil {
			t.Fatalf("cut confirm: %v", err)
		}
		gen = currentGen(t, s, id)
	}
	return id, gen
}

func sealSamples(gen task.Generation) SampleSealRequest {
	return SampleSealRequest{
		Generation: gen,
		Samples:    []SampleBinding{{BasketSeal: "seal-1", BlindCode: "code-1"}, {BasketSeal: "seal-2", BlindCode: "code-2"}},
	}
}

func TestConcurrentShedOccupationOnlyOneWins(t *testing.T) {
	s := newFileStore(t)
	reqA := defaultLockRequest()
	reqA.HealingShed = "shed-1"
	reqB := defaultLockRequest()
	reqB.Batch = "B-2"
	reqB.BasketSeals = []string{"seal-3", "seal-4"}
	reqB.BlindCodes = []string{"code-3", "code-4"}
	reqB.HealingShed = "shed-1" // same physical shed

	idA, genA := advanceToSealing(t, s, reqA)
	idB, genB := advanceToSealing(t, s, reqB)

	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, results[0] = s.SampleSeal(context.Background(), idA, sealSamples(genA))
	}()
	go func() {
		defer wg.Done()
		<-start
		_, results[1] = s.SampleSeal(context.Background(), idB, sealSamples(genB))
	}()
	close(start)
	wg.Wait()

	var ok, fail int
	for _, err := range results {
		if err == nil {
			ok++
		} else {
			fail++
		}
	}
	if ok != 1 || fail != 1 {
		t.Fatalf("shed concurrency: %d ok, %d fail; want exactly 1 ok and 1 fail", ok, fail)
	}
	owner, held := s.Held(ledger.ResourceHealingShed, "shed-1")
	if !held {
		t.Fatal("shed must be held by the winning task")
	}
	if owner != idA && owner != idB {
		t.Fatalf("shed owner = %q; want one of the two tasks", owner)
	}
}

func TestConcurrentWellOccupationLeavesNoLease(t *testing.T) {
	s := newFileStore(t)
	reqA := defaultLockRequest()
	reqB := defaultLockRequest()
	reqB.Batch = "B-2"
	reqB.BasketSeals = []string{"seal-3", "seal-4"}
	reqB.BlindCodes = []string{"code-3", "code-4"}
	// Both tasks want the same test well.
	reqA.TestWells = []string{"well-1"}
	reqB.TestWells = []string{"well-1"}

	idA, genA := advanceToSealing(t, s, reqA)
	idB, genB := advanceToSealing(t, s, reqB)

	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, results[0] = s.SampleSeal(context.Background(), idA, sealSamples(genA))
	}()
	go func() {
		defer wg.Done()
		<-start
		_, results[1] = s.SampleSeal(context.Background(), idB, sealSamples(genB))
	}()
	close(start)
	wg.Wait()

	// The losing task must have no basket samples (its whole write rolled back).
	var loser task.ID
	if results[0] != nil {
		loser = idA
	} else {
		loser = idB
	}
	samples, err := loadBasketSamples(context.Background(), s.db, loser)
	if err != nil {
		t.Fatalf("load samples: %v", err)
	}
	if len(samples) != 0 {
		t.Fatalf("losing task left %d basket samples; want 0", len(samples))
	}
	_, held := s.Held(ledger.ResourceTestWell, "well-1")
	if !held {
		t.Fatal("test well must be held by the winning task")
	}
}
