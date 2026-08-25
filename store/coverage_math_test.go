package store

import (
	"context"
	"errors"
	"math"
	"testing"

	"potatoeye-cutseed-sprout-gate/evidence"
	"potatoeye-cutseed-sprout-gate/fixed"
	"potatoeye-cutseed-sprout-gate/task"
)

func advanceToObserve(t *testing.T, s *SQLite) (task.ID, task.Generation) {
	t.Helper()
	id, gen := advanceToSealing(t, s, defaultLockRequest())
	if _, err := s.SampleSeal(context.Background(), id, sealSamples(gen)); err != nil {
		t.Fatalf("sample seal: %v", err)
	}
	return id, currentGen(t, s, id)
}

func observeReq(gen task.Generation, cells []ObserveCell) ObserveRequest {
	return ObserveRequest{
		Generation: gen, Cells: cells,
		ShedTemp: fixed.New(20000), Humidity: fixed.New(60000), WeightChange: fixed.New(1000),
	}
}

func TestCoverageMissingRejected(t *testing.T) {
	s := newMemStore(t)
	id, gen := advanceToObserve(t, s)
	// Drop one cell so the matrix is incomplete.
	cells := observeCells()[:3]
	_, err := s.Observe(context.Background(), id, 1, observeReq(gen, cells))
	if err == nil {
		t.Fatal("missing coverage cell must be rejected")
	}
	var se *Error
	if !errors.As(err, &se) || se.Code != CodeInvalidRequest {
		t.Fatalf("err = %v; want INVALID_REQUEST", err)
	}
}

func TestDuplicateCoverageRejected(t *testing.T) {
	s := newMemStore(t)
	id, gen := advanceToObserve(t, s)
	cells := observeCells()
	cells = append(cells, cells[0]) // duplicate (seal, point)
	_, err := s.Observe(context.Background(), id, 1, observeReq(gen, cells))
	if err == nil {
		t.Fatal("duplicate coverage cell must be rejected")
	}
	var se *Error
	if !errors.As(err, &se) || se.Code != CodeDuplicateKey {
		t.Fatalf("err = %v; want DUPLICATE_KEY", err)
	}
}

func TestConservationViolationRejected(t *testing.T) {
	s := newMemStore(t)
	id, gen := advanceToObserve(t, s)
	cells := observeCells()
	cells[0].SuberizedBlocks = 9 // 9+1+1 != 10
	_, err := s.Observe(context.Background(), id, 1, observeReq(gen, cells))
	if err == nil {
		t.Fatal("non-conserved counts must be rejected")
	}
	var se *Error
	if !errors.As(err, &se) || se.Code != CodeInvalidRequest {
		t.Fatalf("err = %v; want INVALID_REQUEST", err)
	}
}

func TestBudEyeBoundsRejected(t *testing.T) {
	s := newMemStore(t)
	id, gen := advanceToObserve(t, s)
	if _, err := s.Observe(context.Background(), id, 1, observeReq(gen, observeCells())); err != nil {
		t.Fatalf("observe: %v", err)
	}
	gen = currentGen(t, s, id)
	// active 11 exceeds capacity 10.
	_, err := s.VerifyBudEyes(context.Background(), id, BudEyeRequest{
		Generation: gen,
		Counts:     []BudEyeCount{{BasketSeal: "seal-1", PointID: "p-1", Active: 11, Capacity: 10}},
	})
	if err == nil {
		t.Fatal("out-of-bounds bud-eye count must be rejected")
	}
	var se *Error
	if !errors.As(err, &se) || se.Code != CodeInvalidRequest {
		t.Fatalf("err = %v; want INVALID_REQUEST", err)
	}
}

func TestFixedPointBoundariesPass(t *testing.T) {
	rate, err := computeWaterLossRate(1, 10)
	if err != nil || rate.Raw() != 100 {
		t.Fatalf("water loss rate = %v, %v; want 100", rate, err)
	}
	if err := evidence.ValidateShedTemp(fixed.New(99999)); err != nil {
		t.Fatalf("shed temp boundary must pass: %v", err)
	}
	if err := evidence.ValidateHumidity(fixed.New(100000)); err != nil {
		t.Fatalf("humidity boundary must pass: %v", err)
	}
}

func TestFixedPointOverflowAndSignRejected(t *testing.T) {
	if _, err := fixed.New(math.MaxInt64).Add(fixed.New(1)); !errors.Is(err, fixed.ErrOverflow) {
		t.Fatalf("add overflow err = %v; want ErrOverflow", err)
	}
	if _, err := fixed.New(1).Div(fixed.New(0)); !errors.Is(err, fixed.ErrDivideZero) {
		t.Fatalf("div zero err = %v; want ErrDivideZero", err)
	}
	if err := evidence.ValidateHumidity(fixed.New(-1)); !errors.Is(err, evidence.ErrReadingOutOfRange) {
		t.Fatalf("negative humidity err = %v; want out of range", err)
	}
	if err := evidence.ValidateWeightChange(fixed.New(-1)); !errors.Is(err, evidence.ErrReadingOutOfRange) {
		t.Fatalf("negative weight change err = %v; want out of range", err)
	}
}
