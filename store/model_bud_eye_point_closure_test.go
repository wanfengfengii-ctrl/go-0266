package store_test

import (
	"context"
	"errors"
	"testing"

	"potatoeye-cutseed-sprout-gate/arbiter"
	"potatoeye-cutseed-sprout-gate/catalog"
	"potatoeye-cutseed-sprout-gate/evidence"
	"potatoeye-cutseed-sprout-gate/fixed"
	"potatoeye-cutseed-sprout-gate/store"
	"potatoeye-cutseed-sprout-gate/task"
)

func TestModel_BudEyeVerificationRequiresEveryLockedPoint(t *testing.T) {
	ctx := context.Background()
	lockedSeals := []string{"seal-1", "seal-2"}
	lockedPoints := []string{"p-1", "p-2"}
	observedDays := []evidence.AgeDay{1, 2, 3}

	countsFor := func(points ...string) []store.BudEyeCount {
		counts := make([]store.BudEyeCount, 0, len(lockedSeals)*len(points))
		for _, seal := range lockedSeals {
			for _, point := range points {
				counts = append(counts, store.BudEyeCount{
					BasketSeal: seal,
					PointID:    point,
					Active:     3,
					Capacity:   10,
				})
			}
		}
		return counts
	}

	observations := make([]store.ObserveCell, 0, len(lockedSeals)*len(lockedPoints))
	for _, seal := range lockedSeals {
		for _, point := range lockedPoints {
			observations = append(observations, store.ObserveCell{
				BasketSeal:        seal,
				PointID:           point,
				SuberizationGrade: 2,
				WaterLossBlocks:   1,
				RotBlocks:         1,
				SuberizedBlocks:   8,
				TotalBlocks:       10,
			})
		}
	}

	cases := []struct {
		name              string
		budEyeSubmissions [][]store.BudEyeCount
		wantReady         bool
	}{
		{
			name:              "missing locked p2 point keeps final evidence open",
			budEyeSubmissions: [][]store.BudEyeCount{countsFor("p-1")},
		},
		{
			name:              "all locked points close across observed days",
			budEyeSubmissions: [][]store.BudEyeCount{countsFor("p-1"), countsFor("p-2")},
			wantReady:         true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := store.OpenMemory()
			if err != nil {
				t.Fatalf("open memory store: %v", err)
			}
			t.Cleanup(func() { _ = s.Close() })

			locked, err := s.Lock(ctx, store.LockRequest{
				Plot:        "P-01",
				Variety:     "V-G2",
				Cellar:      "C-01",
				Disinfect:   "D-01",
				Batch:       "B-1",
				BasketSeals: lockedSeals,
				BlindCodes:  []string{"code-1", "code-2"},
				HealingShed: "shed-1",
				ProbeWindow: "probe-1",
				SproutSlot:  "slot-1",
				TestWells:   []string{"well-1"},
				Points:      lockedPoints,
				Reviewers:   []catalog.PersonnelID{"reviewer-1", "reviewer-2"},
			})
			if err != nil {
				t.Fatalf("lock: %v", err)
			}

			current := func() store.TaskDetail {
				t.Helper()
				detail, err := s.GetTask(ctx, locked.TaskID)
				if err != nil {
					t.Fatalf("get task: %v", err)
				}
				return detail
			}

			gen := locked.Generation
			snapshot := current().Task.Snapshot
			for _, cutter := range []catalog.PersonnelID{"cutter-1", "cutter-2"} {
				if _, err := s.CutConfirm(ctx, locked.TaskID, store.CutConfirmRequest{
					PersonnelID: cutter,
					Generation:  gen,
					Snapshot:    snapshot,
				}); err != nil {
					t.Fatalf("cut confirm %s: %v", cutter, err)
				}
				gen = current().Task.Generation
			}

			if _, err := s.SampleSeal(ctx, locked.TaskID, store.SampleSealRequest{
				Generation: gen,
				Samples: []store.SampleBinding{
					{BasketSeal: "seal-1", BlindCode: "code-1"},
					{BasketSeal: "seal-2", BlindCode: "code-2"},
				},
			}); err != nil {
				t.Fatalf("sample seal: %v", err)
			}
			gen = current().Task.Generation

			for _, day := range observedDays {
				if _, err := s.Observe(ctx, locked.TaskID, day, store.ObserveRequest{
					Generation:   gen,
					Cells:        observations,
					ShedTemp:     fixed.New(20000),
					Humidity:     fixed.New(60000),
					WeightChange: fixed.New(1000),
				}); err != nil {
					t.Fatalf("observe day %d: %v", day, err)
				}
				gen = current().Task.Generation
			}

			for i, counts := range tc.budEyeSubmissions {
				got, err := s.VerifyBudEyes(ctx, locked.TaskID, store.BudEyeRequest{
					Generation: gen,
					Counts:     counts,
				})
				if err != nil {
					t.Fatalf("bud-eye submission %d: %v", i+1, err)
				}
				if got.Points != len(counts) {
					t.Fatalf("bud-eye submission %d points = %d; want %d", i+1, got.Points, len(counts))
				}
				gen = current().Task.Generation
			}

			if _, err := s.AddDiseaseEvidence(ctx, locked.TaskID, store.DiseaseRequest{
				Generation:     gen,
				BlindCode:      "code-1",
				WellID:         "well-1",
				LesionDiameter: fixed.New(0),
				ExudateLevel:   0,
				CtValue:        fixed.New(40000),
			}); err != nil {
				t.Fatalf("disease evidence: %v", err)
			}
			gen = current().Task.Generation

			for _, reviewer := range []catalog.PersonnelID{"reviewer-1", "reviewer-2"} {
				if _, err := s.Review(ctx, locked.TaskID, store.ReviewRequest{
					PersonnelID: reviewer,
					Generation:  gen,
					Decision:    arbiter.VerdictReadyToSprout,
				}); err != nil {
					t.Fatalf("review %s: %v", reviewer, err)
				}
				gen = current().Task.Generation
			}

			res, err := s.FinalizeTask(ctx, locked.TaskID, store.FinalizeRequest{Generation: gen})
			if tc.wantReady {
				if err != nil {
					t.Fatalf("finalize: %v", err)
				}
				if res.Verdict != arbiter.VerdictReadyToSprout || res.Credential == "" {
					t.Fatalf("finalize result = %+v; want ready_to_sprout with credential", res)
				}
				return
			}

			if err == nil {
				t.Fatalf("finalize with missing p-2 bud-eye counts succeeded: verdict=%s credential=%q", res.Verdict, res.Credential)
			}
			var se *store.Error
			if !errors.As(err, &se) || se.Code != store.CodeNotFinalizable {
				t.Fatalf("finalize err = %v; want NOT_FINALIZABLE", err)
			}
			if state := current().Task.State; state == task.StateReadyToSprout {
				t.Fatalf("state = %s; missing p-2 point must not produce ready_to_sprout", state)
			}
		})
	}
}
