package store_test

import (
	"context"
	"testing"

	"potatoeye-cutseed-sprout-gate/arbiter"
	"potatoeye-cutseed-sprout-gate/catalog"
	"potatoeye-cutseed-sprout-gate/evidence"
	"potatoeye-cutseed-sprout-gate/fixed"
	"potatoeye-cutseed-sprout-gate/store"
	"potatoeye-cutseed-sprout-gate/task"
)

func TestModel_FinalizeUsesReviewerDecisions(t *testing.T) {
	ctx := context.Background()
	reviewers := []catalog.PersonnelID{"reviewer-1", "reviewer-2"}

	current := func(t *testing.T, s *store.SQLite, id task.ID) store.TaskDetail {
		t.Helper()
		detail, err := s.GetTask(ctx, id)
		if err != nil {
			t.Fatalf("get task: %v", err)
		}
		return detail
	}

	observeCells := func() []store.ObserveCell {
		var cells []store.ObserveCell
		for _, seal := range []string{"seal-1", "seal-2"} {
			for _, point := range []string{"p-1", "p-2"} {
				cells = append(cells, store.ObserveCell{
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
		return cells
	}

	budEyeCounts := func() []store.BudEyeCount {
		var counts []store.BudEyeCount
		for _, seal := range []string{"seal-1", "seal-2"} {
			for _, point := range []string{"p-1", "p-2"} {
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

	cases := []struct {
		name      string
		disease   store.DiseaseRequest
		decisions [2]arbiter.Verdict
		want      arbiter.Verdict
		wantState task.State
	}{
		{
			name: "clean disease evidence and two quarantine reviews",
			disease: store.DiseaseRequest{
				BlindCode:      "code-1",
				WellID:         "well-1",
				LesionDiameter: fixed.New(0),
				ExudateLevel:   0,
				CtValue:        fixed.New(40000),
			},
			decisions: [2]arbiter.Verdict{arbiter.VerdictDiseaseQuarantine, arbiter.VerdictDiseaseQuarantine},
			want:      arbiter.VerdictDiseaseQuarantine,
			wantState: task.StateDiseaseQuarantine,
		},
		{
			name: "clean disease evidence and two sprout reviews",
			disease: store.DiseaseRequest{
				BlindCode:      "code-1",
				WellID:         "well-1",
				LesionDiameter: fixed.New(0),
				ExudateLevel:   0,
				CtValue:        fixed.New(40000),
			},
			decisions: [2]arbiter.Verdict{arbiter.VerdictReadyToSprout, arbiter.VerdictReadyToSprout},
			want:      arbiter.VerdictReadyToSprout,
			wantState: task.StateReadyToSprout,
		},
		{
			name: "positive disease evidence overrides sprout reviews",
			disease: store.DiseaseRequest{
				BlindCode:      "code-1",
				WellID:         "well-1",
				LesionDiameter: fixed.New(0),
				ExudateLevel:   0,
				CtValue:        fixed.New(10000),
			},
			decisions: [2]arbiter.Verdict{arbiter.VerdictReadyToSprout, arbiter.VerdictReadyToSprout},
			want:      arbiter.VerdictDiseaseQuarantine,
			wantState: task.StateDiseaseQuarantine,
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
				BasketSeals: []string{"seal-1", "seal-2"},
				BlindCodes:  []string{"code-1", "code-2"},
				HealingShed: "shed-1",
				ProbeWindow: "probe-1",
				SproutSlot:  "slot-1",
				TestWells:   []string{"well-1"},
				Points:      []string{"p-1", "p-2"},
				Reviewers:   reviewers,
			})
			if err != nil {
				t.Fatalf("lock: %v", err)
			}

			detail := current(t, s, locked.TaskID)
			for _, cutter := range []catalog.PersonnelID{"cutter-1", "cutter-2"} {
				_, err := s.CutConfirm(ctx, locked.TaskID, store.CutConfirmRequest{
					PersonnelID: cutter,
					Generation:  detail.Task.Generation,
					Snapshot:    detail.Task.Snapshot,
				})
				if err != nil {
					t.Fatalf("cut confirm %s: %v", cutter, err)
				}
				detail = current(t, s, locked.TaskID)
			}

			if _, err := s.SampleSeal(ctx, locked.TaskID, store.SampleSealRequest{
				Generation: detail.Task.Generation,
				Samples: []store.SampleBinding{
					{BasketSeal: "seal-1", BlindCode: "code-1"},
					{BasketSeal: "seal-2", BlindCode: "code-2"},
				},
			}); err != nil {
				t.Fatalf("sample seal: %v", err)
			}
			detail = current(t, s, locked.TaskID)

			for _, day := range []int{1, 2, 3} {
				if _, err := s.Observe(ctx, locked.TaskID, evidence.AgeDay(day), store.ObserveRequest{
					Generation:   detail.Task.Generation,
					Cells:        observeCells(),
					ShedTemp:     fixed.New(20000),
					Humidity:     fixed.New(60000),
					WeightChange: fixed.New(1000),
				}); err != nil {
					t.Fatalf("observe day %d: %v", day, err)
				}
				detail = current(t, s, locked.TaskID)
			}

			if _, err := s.VerifyBudEyes(ctx, locked.TaskID, store.BudEyeRequest{
				Generation: detail.Task.Generation,
				Counts:     budEyeCounts(),
			}); err != nil {
				t.Fatalf("bud eyes: %v", err)
			}
			detail = current(t, s, locked.TaskID)

			disease := tc.disease
			disease.Generation = detail.Task.Generation
			if _, err := s.AddDiseaseEvidence(ctx, locked.TaskID, disease); err != nil {
				t.Fatalf("disease evidence: %v", err)
			}
			detail = current(t, s, locked.TaskID)

			for i, decision := range tc.decisions {
				if _, err := s.Review(ctx, locked.TaskID, store.ReviewRequest{
					PersonnelID: reviewers[i],
					Generation:  detail.Task.Generation,
					Decision:    decision,
				}); err != nil {
					t.Fatalf("review %s: %v", reviewers[i], err)
				}
				detail = current(t, s, locked.TaskID)
			}

			res, err := s.FinalizeTask(ctx, locked.TaskID, store.FinalizeRequest{Generation: detail.Task.Generation})
			if err != nil {
				t.Fatalf("finalize: %v", err)
			}
			if res.Verdict != tc.want {
				t.Fatalf("verdict = %s; want %s", res.Verdict, tc.want)
			}
			if res.Credential == "" {
				t.Fatal("final verdict must carry a terminal credential")
			}
			detail = current(t, s, locked.TaskID)
			if detail.Task.State != tc.wantState {
				t.Fatalf("task state = %s; want %s", detail.Task.State, tc.wantState)
			}
		})
	}
}
