package store

import (
	"context"
	"errors"
	"testing"

	"potatoeye-cutseed-sprout-gate/catalog"
	"potatoeye-cutseed-sprout-gate/task"
)

func TestModel_CutConfirmRequiresLockedSnapshot(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name    string
		first   catalog.PersonnelID
		second  catalog.PersonnelID
		mutate  func(*task.LockSnapshot)
		wantErr bool
	}{
		{
			name:   "matching locked snapshot completes with two cutters",
			first:  "cutter-1",
			second: "cutter-2",
		},
		{
			name:    "plot mismatch rejected before second cutter writes",
			first:   "cutter-1",
			second:  "cutter-2",
			mutate:  func(s *task.LockSnapshot) { s.Plot = "P-02" },
			wantErr: true,
		},
		{
			name:    "variety mismatch rejected before second cutter writes",
			first:   "cutter-1",
			second:  "cutter-2",
			mutate:  func(s *task.LockSnapshot) { s.Variety = "V-G3" },
			wantErr: true,
		},
		{
			name:    "cellar mismatch rejected before second cutter writes",
			first:   "cutter-1",
			second:  "cutter-2",
			mutate:  func(s *task.LockSnapshot) { s.CellarSummary = "C-STALE" },
			wantErr: true,
		},
		{
			name:    "disinfect batch mismatch rejected before second cutter writes",
			first:   "cutter-1",
			second:  "cutter-2",
			mutate:  func(s *task.LockSnapshot) { s.DisinfectBatch = "D-STALE" },
			wantErr: true,
		},
		{
			name:   "threshold mismatch rejected before second cutter writes",
			first:  "cutter-1",
			second: "cutter-2",
			mutate: func(s *task.LockSnapshot) {
				s.Thresholds.SuberizationGrade++
				s.Thresholds.WaterLossRate++
				s.Thresholds.DiseaseCt++
			},
			wantErr: true,
		},
		{
			name:    "reviewer list mismatch rejected before second cutter writes",
			first:   "cutter-1",
			second:  "cutter-2",
			mutate:  func(s *task.LockSnapshot) { s.Reviewers = []catalog.PersonnelID{"reviewer-1", "reviewer-stale"} },
			wantErr: true,
		},
		{
			name:    "first cutter mismatch is not recorded",
			second:  "cutter-1",
			mutate:  func(s *task.LockSnapshot) { s.CellarSummary = "C-STALE" },
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newMemStore(t)
			id, gen := lockDefault(t, s)
			locked := currentSnapshot(t, s, id)

			if tc.first != "" {
				res, err := s.CutConfirm(ctx, id, CutConfirmRequest{
					PersonnelID: tc.first,
					Generation:  gen,
					Snapshot:    locked,
				})
				if err != nil {
					t.Fatalf("first confirm: %v", err)
				}
				if res.Complete {
					t.Fatalf("first confirm complete = true; want false")
				}
				gen = currentGen(t, s, id)
			}

			submitted := locked
			if tc.mutate != nil {
				tc.mutate(&submitted)
			}

			before, err := s.GetTask(ctx, id)
			if err != nil {
				t.Fatalf("get before: %v", err)
			}

			res, err := s.CutConfirm(ctx, id, CutConfirmRequest{
				PersonnelID: tc.second,
				Generation:  before.Task.Generation,
				Snapshot:    submitted,
			})
			if tc.wantErr {
				if err == nil {
					t.Fatalf("mismatched confirm returned %+v; want conflict", res)
				}
				var se *Error
				if !errors.As(err, &se) || se.Code != CodeConflict {
					t.Fatalf("err = %v; want CONFLICT", err)
				}
				after, err := s.GetTask(ctx, id)
				if err != nil {
					t.Fatalf("get after mismatch: %v", err)
				}
				if after.Task.State != task.StatePendingCutConfirm {
					t.Fatalf("state after mismatch = %s; want %s", after.Task.State, task.StatePendingCutConfirm)
				}
				if after.Task.Generation != before.Task.Generation {
					t.Fatalf("generation after mismatch = %d; want %d", after.Task.Generation, before.Task.Generation)
				}
				res, err = s.CutConfirm(ctx, id, CutConfirmRequest{
					PersonnelID: tc.second,
					Generation:  before.Task.Generation,
					Snapshot:    locked,
				})
				if err != nil {
					t.Fatalf("correct retry after mismatch: %v", err)
				}
			} else if err != nil {
				t.Fatalf("second confirm: %v", err)
			}

			wantComplete := tc.first != ""
			if res.Complete != wantComplete {
				t.Fatalf("complete = %t; want %t", res.Complete, wantComplete)
			}
			after, err := s.GetTask(ctx, id)
			if err != nil {
				t.Fatalf("get final: %v", err)
			}
			wantState := task.StatePendingCutConfirm
			wantGen := before.Task.Generation
			if wantComplete {
				wantState = task.StateSealingSamples
				wantGen++
			}
			if after.Task.State != wantState {
				t.Fatalf("final state = %s; want %s", after.Task.State, wantState)
			}
			if after.Task.Generation != wantGen {
				t.Fatalf("final generation = %d; want %d", after.Task.Generation, wantGen)
			}
		})
	}
}
