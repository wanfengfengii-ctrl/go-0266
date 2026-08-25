package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"potatoeye-cutseed-sprout-gate/catalog"
	"potatoeye-cutseed-sprout-gate/task"
)

func TestModel_CutConfirmIdempotencyReplayStableAfterAdvance(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name                string
		restartBeforeReplay bool
	}{
		{name: "same process", restartBeforeReplay: false},
		{name: "after restart", restartBeforeReplay: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "store.db")
			s, err := Open(path)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			defer func() { _ = s.Close() }()

			id, gen := lockDefault(t, s)
			snap := currentSnapshot(t, s, id)
			firstReq := CutConfirmRequest{
				OperationID: "cut-confirm-cutter-1",
				PersonnelID: "cutter-1",
				Generation:  gen,
				Snapshot:    snap,
			}

			firstRes, err := s.CutConfirm(ctx, id, firstReq)
			if err != nil {
				t.Fatalf("first confirm: %v", err)
			}
			assertCutConfirmResult(t, firstRes, []catalog.PersonnelID{"cutter-1"}, false)

			secondRes, err := s.CutConfirm(ctx, id, CutConfirmRequest{
				OperationID: "cut-confirm-cutter-2",
				PersonnelID: "cutter-2",
				Generation:  gen,
				Snapshot:    snap,
			})
			if err != nil {
				t.Fatalf("second confirm: %v", err)
			}
			assertCutConfirmResult(t, secondRes, []catalog.PersonnelID{"cutter-1", "cutter-2"}, true)
			assertTaskState(t, s, id, task.StateSealingSamples, gen+1)

			if tc.restartBeforeReplay {
				if err := s.Close(); err != nil {
					t.Fatalf("close before replay: %v", err)
				}
				s, err = Open(path)
				if err != nil {
					t.Fatalf("reopen before replay: %v", err)
				}
			}

			replayed, err := s.CutConfirm(ctx, id, firstReq)
			if err != nil {
				t.Fatalf("replay first confirm: %v", err)
			}
			assertCutConfirmResult(t, replayed, []catalog.PersonnelID{"cutter-1"}, false)

			conflictingReq := firstReq
			conflictingReq.Snapshot.Plot = "P-99"
			_, err = s.CutConfirm(ctx, id, conflictingReq)
			if err == nil {
				t.Fatal("same operation id with different content must conflict")
			}
			var se *Error
			if !errors.As(err, &se) || se.Code != CodeConflict {
				t.Fatalf("err = %v; want CONFLICT", err)
			}
			assertTaskState(t, s, id, task.StateSealingSamples, gen+1)
		})
	}
}

func assertCutConfirmResult(t *testing.T, got CutConfirmResult, wantConfirmed []catalog.PersonnelID, wantComplete bool) {
	t.Helper()
	if got.Complete != wantComplete {
		t.Fatalf("complete = %v; want %v (result %+v)", got.Complete, wantComplete, got)
	}
	if len(got.Confirmed) != len(wantConfirmed) {
		t.Fatalf("confirmed = %v; want %v", got.Confirmed, wantConfirmed)
	}
	for i := range wantConfirmed {
		if got.Confirmed[i] != wantConfirmed[i] {
			t.Fatalf("confirmed = %v; want %v", got.Confirmed, wantConfirmed)
		}
	}
}

func assertTaskState(t *testing.T, s *SQLite, id task.ID, wantState task.State, wantGen task.Generation) {
	t.Helper()
	detail, err := s.GetTask(context.Background(), id)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if detail.Task.State != wantState || detail.Task.Generation != wantGen {
		t.Fatalf("task state/generation = %s/%d; want %s/%d",
			detail.Task.State, detail.Task.Generation, wantState, wantGen)
	}
}
