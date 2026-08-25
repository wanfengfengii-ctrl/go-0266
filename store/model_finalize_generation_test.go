package store

import (
	"context"
	"errors"
	"sync"
	"testing"

	"potatoeye-cutseed-sprout-gate/arbiter"
	"potatoeye-cutseed-sprout-gate/catalog"
	"potatoeye-cutseed-sprout-gate/task"
)

func TestModel_FinalizeRequiresCurrentGenerationAtTerminalBoundary(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "stale cancel after cut confirmations leaves task open and undecided",
			run: func(t *testing.T) {
				s := newMemStore(t)
				id, staleGen := lockDefault(t, s)
				advancePastCutConfirmations(t, ctx, s, id)
				before := taskProjection(t, ctx, s, id)

				res, err := s.FinalizeTask(ctx, id, FinalizeRequest{Generation: staleGen, Cancelled: true})
				requireStoreCode(t, err, CodeStaleGeneration)
				if res.Credential != "" {
					t.Fatalf("stale cancel returned credential %q; want none", res.Credential)
				}
				requireNoFinalDecision(t, ctx, s, id)
				requireTaskState(t, ctx, s, id, before.State, before.Generation)
			},
		},
		{
			name: "stale ordinary finalize leaves reviewed task open and undecided",
			run: func(t *testing.T) {
				s := newMemStore(t)
				id, _ := lockDefault(t, s)
				happyPath(t, s, id)
				before := taskProjection(t, ctx, s, id)

				res, err := s.FinalizeTask(ctx, id, FinalizeRequest{Generation: before.Generation - 1})
				requireStoreCode(t, err, CodeStaleGeneration)
				if res.Credential != "" {
					t.Fatalf("stale ordinary finalize returned credential %q; want none", res.Credential)
				}
				requireNoFinalDecision(t, ctx, s, id)
				requireTaskState(t, ctx, s, id, before.State, before.Generation)

				res, err = s.FinalizeTask(ctx, id, FinalizeRequest{Generation: before.Generation})
				if err != nil {
					t.Fatalf("current-generation ordinary finalize: %v", err)
				}
				if res.Verdict != arbiter.VerdictReadyToSprout || res.Credential == "" {
					t.Fatalf("ordinary finalize result = %+v; want ready_to_sprout with credential", res)
				}
			},
		},
		{
			name: "current generation cancel from open state writes one terminal decision",
			run: func(t *testing.T) {
				s := newMemStore(t)
				id, _ := lockDefault(t, s)
				advancePastCutConfirmations(t, ctx, s, id)
				before := taskProjection(t, ctx, s, id)

				res, err := s.FinalizeTask(ctx, id, FinalizeRequest{Generation: before.Generation, Cancelled: true})
				if err != nil {
					t.Fatalf("current-generation cancel: %v", err)
				}
				if res.Verdict != arbiter.VerdictCancelled || res.Credential == "" {
					t.Fatalf("cancel result = %+v; want cancelled with credential", res)
				}
				requireTaskState(t, ctx, s, id, task.StateCancelled, before.Generation+1)

				decision, found, err := loadFinalDecision(ctx, s.db, id)
				if err != nil {
					t.Fatalf("load final decision: %v", err)
				}
				if !found {
					t.Fatal("current-generation cancel did not write final decision")
				}
				if decision.Verdict != arbiter.VerdictCancelled || decision.Credential != res.Credential || decision.Generation != before.Generation+1 {
					t.Fatalf("final decision = %+v; want cancelled generation %d with credential %q", decision, before.Generation+1, res.Credential)
				}
			},
		},
		{
			name: "current generation concurrent terminal attempts only commit once",
			run: func(t *testing.T) {
				s := newFileStore(t)
				id, _ := lockDefault(t, s)
				happyPath(t, s, id)
				gen := currentGen(t, s, id)

				var wg sync.WaitGroup
				start := make(chan struct{})
				errs := make([]error, 2)
				wg.Add(2)
				go func() {
					defer wg.Done()
					<-start
					_, errs[0] = s.FinalizeTask(ctx, id, FinalizeRequest{Generation: gen})
				}()
				go func() {
					defer wg.Done()
					<-start
					_, errs[1] = s.FinalizeTask(ctx, id, FinalizeRequest{Generation: gen, Cancelled: true})
				}()
				close(start)
				wg.Wait()

				successes := 0
				for _, err := range errs {
					if err == nil {
						successes++
					}
				}
				if successes != 1 {
					t.Fatalf("concurrent terminal attempts produced %d successes; want exactly 1 (errs=%v)", successes, errs)
				}
				decision, found, err := loadFinalDecision(ctx, s.db, id)
				if err != nil {
					t.Fatalf("load final decision: %v", err)
				}
				if !found || decision.Credential == "" {
					t.Fatalf("final decision = %+v found=%v; want one credentialed decision", decision, found)
				}
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, tc.run)
	}
}

func advancePastCutConfirmations(t *testing.T, ctx context.Context, s *SQLite, id task.ID) {
	t.Helper()
	snap := currentSnapshot(t, s, id)
	gen := currentGen(t, s, id)
	for _, p := range []catalog.PersonnelID{"cutter-1", "cutter-2"} {
		if _, err := s.CutConfirm(ctx, id, CutConfirmRequest{PersonnelID: p, Generation: gen, Snapshot: snap}); err != nil {
			t.Fatalf("cut confirm %s: %v", p, err)
		}
		gen = currentGen(t, s, id)
	}
}

func taskProjection(t *testing.T, ctx context.Context, s *SQLite, id task.ID) task.Task {
	t.Helper()
	detail, err := s.GetTask(ctx, id)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	return detail.Task
}

func requireTaskState(t *testing.T, ctx context.Context, s *SQLite, id task.ID, wantState task.State, wantGen task.Generation) {
	t.Helper()
	got := taskProjection(t, ctx, s, id)
	if got.State != wantState || got.Generation != wantGen {
		t.Fatalf("task state/generation = %s/%d; want %s/%d", got.State, got.Generation, wantState, wantGen)
	}
}

func requireNoFinalDecision(t *testing.T, ctx context.Context, s *SQLite, id task.ID) {
	t.Helper()
	decision, found, err := loadFinalDecision(ctx, s.db, id)
	if err != nil {
		t.Fatalf("load final decision: %v", err)
	}
	if found {
		t.Fatalf("unexpected final decision after stale finalize: %+v", decision)
	}
}

func requireStoreCode(t *testing.T, err error, want Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil; want %s", want)
	}
	var storeErr *Error
	if !errors.As(err, &storeErr) || storeErr.Code != want {
		t.Fatalf("error = %v; want code %s", err, want)
	}
}
