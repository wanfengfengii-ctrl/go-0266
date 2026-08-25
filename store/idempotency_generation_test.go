package store

import (
	"context"
	"errors"
	"testing"

	"potatoeye-cutseed-sprout-gate/arbiter"
	"potatoeye-cutseed-sprout-gate/catalog"
	"potatoeye-cutseed-sprout-gate/task"
)

func TestCutConfirmIdempotentRetry(t *testing.T) {
	s := newMemStore(t)
	id, gen := lockDefault(t, s)
	snap := currentSnapshot(t, s, id)
	ctx := context.Background()

	req := CutConfirmRequest{OperationID: "op-1", PersonnelID: "cutter-1", Generation: gen, Snapshot: snap}
	if _, err := s.CutConfirm(ctx, id, req); err != nil {
		t.Fatalf("first confirm: %v", err)
	}
	// Same operation id, same content: replay the same result without adding a
	// second confirmation from cutter-1.
	res, err := s.CutConfirm(ctx, id, req)
	if err != nil {
		t.Fatalf("replay confirm: %v", err)
	}
	if len(res.Confirmed) != 1 || res.Complete {
		t.Fatalf("replay result = %+v; want exactly cutter-1 and incomplete", res)
	}
}

func TestCutConfirmIdempotentConflict(t *testing.T) {
	s := newMemStore(t)
	id, gen := lockDefault(t, s)
	snap := currentSnapshot(t, s, id)
	ctx := context.Background()

	if _, err := s.CutConfirm(ctx, id, CutConfirmRequest{OperationID: "op-1", PersonnelID: "cutter-1", Generation: gen, Snapshot: snap}); err != nil {
		t.Fatalf("first confirm: %v", err)
	}
	changed := snap
	changed.Plot = "P-OTHER"
	_, err := s.CutConfirm(ctx, id, CutConfirmRequest{OperationID: "op-1", PersonnelID: "cutter-1", Generation: gen, Snapshot: changed})
	if err == nil {
		t.Fatal("same operation id with different content must conflict")
	}
	var se *Error
	if !errors.As(err, &se) || se.Code != CodeConflict {
		t.Fatalf("err = %v; want CONFLICT", err)
	}
}

func TestWrongGenerationRejected(t *testing.T) {
	s := newMemStore(t)
	id, _ := lockDefault(t, s)
	snap := currentSnapshot(t, s, id)
	_, err := s.CutConfirm(context.Background(), id, CutConfirmRequest{
		PersonnelID: "cutter-1", Generation: 99, Snapshot: snap,
	})
	if err == nil {
		t.Fatal("stale generation must be rejected")
	}
	var se *Error
	if !errors.As(err, &se) || se.Code != CodeStaleGeneration {
		t.Fatalf("err = %v; want STALE_GENERATION", err)
	}
	detail, err := s.GetTask(context.Background(), id)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if detail.Task.State != task.StatePendingCutConfirm {
		t.Fatalf("state = %s; want pending_cut_confirm unchanged", detail.Task.State)
	}
}

func TestReviewerCutterOverlapRejected(t *testing.T) {
	s := newMemStore(t)
	id, _ := lockDefault(t, s)
	driveToDiseaseRetesting(t, s, id)
	gen := currentGen(t, s, id)
	_, err := s.Review(context.Background(), id, ReviewRequest{
		PersonnelID: catalog.PersonnelID("cutter-1"), Generation: gen,
		Decision: arbiter.VerdictReadyToSprout,
	})
	if err == nil {
		t.Fatal("reviewer overlapping with cutter must be rejected")
	}
	var se *Error
	if !errors.As(err, &se) || se.Code != CodeConflict {
		t.Fatalf("err = %v; want CONFLICT", err)
	}
}
