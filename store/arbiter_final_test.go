package store

import (
	"context"
	"errors"
	"sync"
	"testing"

	"potatoeye-cutseed-sprout-gate/arbiter"
	"potatoeye-cutseed-sprout-gate/catalog"
	"potatoeye-cutseed-sprout-gate/fixed"
)

func TestTwoReviewsReadyToSprout(t *testing.T) {
	s := newMemStore(t)
	id, _ := lockDefault(t, s)
	happyPath(t, s, id)
	gen := currentGen(t, s, id)

	res, err := s.FinalizeTask(context.Background(), id, FinalizeRequest{Generation: gen})
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if res.Verdict != arbiter.VerdictReadyToSprout {
		t.Fatalf("verdict = %s; want ready_to_sprout", res.Verdict)
	}
	if res.Credential == "" {
		t.Fatal("ready_to_sprout must carry a terminal credential")
	}
}

func TestDiseasePositiveQuarantine(t *testing.T) {
	s := newMemStore(t)
	id, _ := lockDefault(t, s)
	driveToDisease(t, s, id, DiseaseRequest{
		BlindCode: "code-1", WellID: "well-1",
		LesionDiameter: fixed.New(0), ExudateLevel: 0, CtValue: fixed.New(10000),
	})
	ctx := context.Background()
	gen := currentGen(t, s, id)
	for _, p := range []catalog.PersonnelID{"reviewer-1", "reviewer-2"} {
		if _, err := s.Review(ctx, id, ReviewRequest{PersonnelID: p, Generation: gen, Decision: arbiter.VerdictReadyToSprout}); err != nil {
			t.Fatalf("review %s: %v", p, err)
		}
		gen = currentGen(t, s, id)
	}
	res, err := s.FinalizeTask(ctx, id, FinalizeRequest{Generation: gen})
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if res.Verdict != arbiter.VerdictDiseaseQuarantine {
		t.Fatalf("verdict = %s; want disease_quarantine", res.Verdict)
	}
}

func TestLateAdvanceAfterTerminalRejected(t *testing.T) {
	s := newMemStore(t)
	id, _ := lockDefault(t, s)
	driveToDisease(t, s, id, DiseaseRequest{
		BlindCode: "code-1", WellID: "well-1",
		LesionDiameter: fixed.New(0), ExudateLevel: 0, CtValue: fixed.New(10000),
	})
	ctx := context.Background()
	gen := currentGen(t, s, id)
	for _, p := range []catalog.PersonnelID{"reviewer-1", "reviewer-2"} {
		if _, err := s.Review(ctx, id, ReviewRequest{PersonnelID: p, Generation: gen, Decision: arbiter.VerdictReadyToSprout}); err != nil {
			t.Fatalf("review: %v", err)
		}
		gen = currentGen(t, s, id)
	}
	if _, err := s.FinalizeTask(ctx, id, FinalizeRequest{Generation: gen}); err != nil {
		t.Fatalf("finalize: %v", err)
	}

	_, err := s.Observe(ctx, id, 3, observeReq(gen+1, observeCells()))
	if err == nil {
		t.Fatal("observation after terminal verdict must be rejected")
	}
	var se *Error
	if !errors.As(err, &se) || se.Code != CodeTerminal {
		t.Fatalf("err = %v; want TERMINAL_STATE", err)
	}
}

func TestOldGenerationLateReadingRejected(t *testing.T) {
	s := newMemStore(t)
	id, _ := lockDefault(t, s)
	happyPath(t, s, id)
	gen := currentGen(t, s, id)
	if _, err := s.FinalizeTask(context.Background(), id, FinalizeRequest{Generation: gen}); err != nil {
		t.Fatalf("finalize: %v", err)
	}

	before, err := loadDisease(context.Background(), s.db, id)
	if err != nil {
		t.Fatalf("load disease: %v", err)
	}
	_, err = s.AddDiseaseEvidence(context.Background(), id, DiseaseRequest{
		Generation: 1, BlindCode: "code-1", WellID: "well-2",
		LesionDiameter: fixed.New(0), ExudateLevel: 0, CtValue: fixed.New(5000),
	})
	if err == nil {
		t.Fatal("stale-generation late reading must be rejected")
	}
	after, err := loadDisease(context.Background(), s.db, id)
	if err != nil {
		t.Fatalf("load disease: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("disease chain grew from %d to %d; must be immutable", len(before), len(after))
	}
}

func TestFinalizeConcurrentSingleTerminal(t *testing.T) {
	s := newFileStore(t)
	id, _ := lockDefault(t, s)
	driveToDisease(t, s, id, DiseaseRequest{
		BlindCode: "code-1", WellID: "well-1",
		LesionDiameter: fixed.New(0), ExudateLevel: 0, CtValue: fixed.New(10000),
	})
	ctx := context.Background()
	gen := currentGen(t, s, id)
	for _, p := range []catalog.PersonnelID{"reviewer-1", "reviewer-2"} {
		if _, err := s.Review(ctx, id, ReviewRequest{PersonnelID: p, Generation: gen, Decision: arbiter.VerdictReadyToSprout}); err != nil {
			t.Fatalf("review: %v", err)
		}
		gen = currentGen(t, s, id)
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, results[0] = s.FinalizeTask(ctx, id, FinalizeRequest{Generation: gen})
	}()
	go func() {
		defer wg.Done()
		<-start
		_, results[1] = s.FinalizeTask(ctx, id, FinalizeRequest{Generation: gen, Cancelled: true})
	}()
	close(start)
	wg.Wait()

	var ok int
	for _, err := range results {
		if err == nil {
			ok++
		}
	}
	if ok != 1 {
		t.Fatalf("concurrent finalize produced %d successful terminal decisions; want exactly 1", ok)
	}
	_, found, err := loadFinalDecision(ctx, s.db, id)
	if err != nil || !found {
		t.Fatalf("final decision found=%v err=%v", found, err)
	}
}
