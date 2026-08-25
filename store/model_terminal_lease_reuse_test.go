package store

import (
	"context"
	"database/sql"
	"testing"

	"potatoeye-cutseed-sprout-gate/arbiter"
	"potatoeye-cutseed-sprout-gate/catalog"
	"potatoeye-cutseed-sprout-gate/evidence"
	"potatoeye-cutseed-sprout-gate/fixed"
	"potatoeye-cutseed-sprout-gate/ledger"
	"potatoeye-cutseed-sprout-gate/task"
)

func TestModel_TerminalFinalizationReleasesPhysicalResourceLeases(t *testing.T) {
	cases := []struct {
		name     string
		finalize func(t *testing.T, ctx context.Context, s *SQLite, id task.ID) arbiter.Verdict
	}{
		{
			name: "cancelled",
			finalize: func(t *testing.T, ctx context.Context, s *SQLite, id task.ID) arbiter.Verdict {
				t.Helper()
				if _, err := s.FinalizeTask(ctx, id, FinalizeRequest{
					Generation: modelGeneration(t, ctx, s, id),
					Cancelled:  true,
				}); err != nil {
					t.Fatalf("cancel first task: %v", err)
				}
				return arbiter.VerdictCancelled
			},
		},
		{
			name: "disease quarantine",
			finalize: func(t *testing.T, ctx context.Context, s *SQLite, id task.ID) arbiter.Verdict {
				t.Helper()
				modelDriveToReviewedDisease(t, ctx, s, id, true)
				res, err := s.FinalizeTask(ctx, id, FinalizeRequest{Generation: modelGeneration(t, ctx, s, id)})
				if err != nil {
					t.Fatalf("finalize disease quarantine: %v", err)
				}
				if res.Verdict != arbiter.VerdictDiseaseQuarantine {
					t.Fatalf("verdict = %s; want %s", res.Verdict, arbiter.VerdictDiseaseQuarantine)
				}
				return res.Verdict
			},
		},
		{
			name: "ready to sprout",
			finalize: func(t *testing.T, ctx context.Context, s *SQLite, id task.ID) arbiter.Verdict {
				t.Helper()
				modelDriveToReviewedDisease(t, ctx, s, id, false)
				res, err := s.FinalizeTask(ctx, id, FinalizeRequest{Generation: modelGeneration(t, ctx, s, id)})
				if err != nil {
					t.Fatalf("finalize ready to sprout: %v", err)
				}
				if res.Verdict != arbiter.VerdictReadyToSprout {
					t.Fatalf("verdict = %s; want %s", res.Verdict, arbiter.VerdictReadyToSprout)
				}
				return res.Verdict
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			s, err := OpenMemory()
			if err != nil {
				t.Fatalf("open memory store: %v", err)
			}
			t.Cleanup(func() { _ = s.Close() })

			physical := []modelPhysicalLease{
				{ledger.ResourceHealingShed, "model-shed-1"},
				{ledger.ResourceProbeWindow, "model-probe-1"},
				{ledger.ResourceSproutSlot, "model-slot-1"},
				{ledger.ResourceTestWell, "model-well-1"},
				{ledger.ResourceTestWell, "model-well-2"},
			}

			first := modelLockRequest("model-batch-a", []string{"model-seal-a1", "model-seal-a2"}, []string{"model-code-a1", "model-code-a2"})
			firstID, firstGen := modelAdvanceToSealing(t, ctx, s, first)
			modelSealSamples(t, ctx, s, firstID, firstGen, first.BasketSeals, first.BlindCodes)
			for _, r := range physical {
				if owner, held := s.Held(r.typ, r.key); !held || owner != firstID {
					t.Fatalf("%s %s owner after first seal = %q held %t; want %q held", r.typ, r.key, owner, held, firstID)
				}
			}

			verdict := tc.finalize(t, ctx, s, firstID)
			finalClock := modelFinalAuditClock(t, ctx, s, firstID, verdict)
			modelAssertReleasedAt(t, ctx, s.db, firstID, physical, finalClock)
			for _, r := range physical {
				if owner, held := s.Held(r.typ, r.key); held {
					t.Fatalf("%s %s still held by %q after %s", r.typ, r.key, owner, verdict)
				}
			}

			successor := modelLockRequest("model-batch-b", []string{"model-seal-b1", "model-seal-b2"}, []string{"model-code-b1", "model-code-b2"})
			successorID, successorGen := modelAdvanceToSealing(t, ctx, s, successor)
			modelSealSamples(t, ctx, s, successorID, successorGen, successor.BasketSeals, successor.BlindCodes)
			for _, r := range physical {
				if owner, held := s.Held(r.typ, r.key); !held || owner != successorID {
					t.Fatalf("%s %s owner after successor seal = %q held %t; want %q held", r.typ, r.key, owner, held, successorID)
				}
			}
		})
	}
}

type modelPhysicalLease struct {
	typ ledger.ResourceType
	key ledger.ResourceKey
}

func modelLockRequest(batch task.BatchNumber, seals, codes []string) LockRequest {
	return LockRequest{
		Plot: "P-01", Variety: "V-G2", Cellar: "C-01", Disinfect: "D-01",
		Batch:       batch,
		BasketSeals: append([]string(nil), seals...),
		BlindCodes:  append([]string(nil), codes...),
		HealingShed: "model-shed-1",
		ProbeWindow: "model-probe-1",
		SproutSlot:  "model-slot-1",
		TestWells:   []string{"model-well-1", "model-well-2"},
		Points:      []string{"model-point-1", "model-point-2"},
		Reviewers:   []catalog.PersonnelID{"reviewer-1", "reviewer-2"},
	}
}

func modelAdvanceToSealing(t *testing.T, ctx context.Context, s *SQLite, req LockRequest) (task.ID, task.Generation) {
	t.Helper()
	res, err := s.Lock(ctx, req)
	if err != nil {
		t.Fatalf("lock %s: %v", req.Batch, err)
	}
	snapshot := modelSnapshot(t, ctx, s, res.TaskID)
	for _, cutter := range []catalog.PersonnelID{"cutter-1", "cutter-2"} {
		if _, err := s.CutConfirm(ctx, res.TaskID, CutConfirmRequest{
			PersonnelID: cutter,
			Generation:  modelGeneration(t, ctx, s, res.TaskID),
			Snapshot:    snapshot,
		}); err != nil {
			t.Fatalf("cut confirm %s: %v", cutter, err)
		}
	}
	return res.TaskID, modelGeneration(t, ctx, s, res.TaskID)
}

func modelSealSamples(t *testing.T, ctx context.Context, s *SQLite, id task.ID, gen task.Generation, seals, codes []string) {
	t.Helper()
	samples := make([]SampleBinding, len(seals))
	for i := range seals {
		samples[i] = SampleBinding{BasketSeal: seals[i], BlindCode: codes[i]}
	}
	if _, err := s.SampleSeal(ctx, id, SampleSealRequest{Generation: gen, Samples: samples}); err != nil {
		t.Fatalf("sample seal %s: %v", id, err)
	}
}

func modelDriveToReviewedDisease(t *testing.T, ctx context.Context, s *SQLite, id task.ID, positive bool) {
	t.Helper()
	for _, day := range []int{1, 2, 3} {
		if _, err := s.Observe(ctx, id, evidence.AgeDay(day), ObserveRequest{
			Generation:   modelGeneration(t, ctx, s, id),
			Cells:        modelObservationCells(),
			ShedTemp:     fixed.New(20000),
			Humidity:     fixed.New(60000),
			WeightChange: fixed.New(1000),
		}); err != nil {
			t.Fatalf("observe day %d: %v", day, err)
		}
	}
	if _, err := s.VerifyBudEyes(ctx, id, BudEyeRequest{
		Generation: modelGeneration(t, ctx, s, id),
		Counts:     modelBudEyeCounts(),
	}); err != nil {
		t.Fatalf("bud-eye verification: %v", err)
	}

	ct := fixed.New(40000)
	if positive {
		ct = fixed.New(10000)
	}
	if _, err := s.AddDiseaseEvidence(ctx, id, DiseaseRequest{
		Generation:     modelGeneration(t, ctx, s, id),
		BlindCode:      "model-code-a1",
		WellID:         "model-well-1",
		LesionDiameter: fixed.New(0),
		ExudateLevel:   0,
		CtValue:        ct,
	}); err != nil {
		t.Fatalf("disease evidence: %v", err)
	}
	for _, reviewer := range []catalog.PersonnelID{"reviewer-1", "reviewer-2"} {
		if _, err := s.Review(ctx, id, ReviewRequest{
			PersonnelID: reviewer,
			Generation:  modelGeneration(t, ctx, s, id),
			Decision:    arbiter.VerdictReadyToSprout,
		}); err != nil {
			t.Fatalf("review %s: %v", reviewer, err)
		}
	}
}

func modelObservationCells() []ObserveCell {
	seals := []string{"model-seal-a1", "model-seal-a2"}
	points := []string{"model-point-1", "model-point-2"}
	cells := make([]ObserveCell, 0, len(seals)*len(points))
	for _, seal := range seals {
		for _, point := range points {
			cells = append(cells, ObserveCell{
				BasketSeal:        seal,
				PointID:           point,
				SuberizationGrade: 2,
				WaterLossBlocks:   1,
				RotBlocks:         0,
				SuberizedBlocks:   9,
				TotalBlocks:       10,
			})
		}
	}
	return cells
}

func modelBudEyeCounts() []BudEyeCount {
	seals := []string{"model-seal-a1", "model-seal-a2"}
	points := []string{"model-point-1", "model-point-2"}
	counts := make([]BudEyeCount, 0, len(seals)*len(points))
	for _, seal := range seals {
		for _, point := range points {
			counts = append(counts, BudEyeCount{BasketSeal: seal, PointID: point, Active: 3, Capacity: 10})
		}
	}
	return counts
}

func modelGeneration(t *testing.T, ctx context.Context, s *SQLite, id task.ID) task.Generation {
	t.Helper()
	detail, err := s.GetTask(ctx, id)
	if err != nil {
		t.Fatalf("get task %s: %v", id, err)
	}
	return detail.Task.Generation
}

func modelSnapshot(t *testing.T, ctx context.Context, s *SQLite, id task.ID) task.LockSnapshot {
	t.Helper()
	detail, err := s.GetTask(ctx, id)
	if err != nil {
		t.Fatalf("get task %s: %v", id, err)
	}
	return detail.Task.Snapshot
}

func modelFinalAuditClock(t *testing.T, ctx context.Context, s *SQLite, id task.ID, verdict arbiter.Verdict) int64 {
	t.Helper()
	events, err := s.Audit(ctx, id)
	if err != nil {
		t.Fatalf("audit %s: %v", id, err)
	}
	want := "finalized " + string(verdict)
	for _, e := range events {
		if e.Reason == want {
			return e.Clock
		}
	}
	t.Fatalf("missing audit reason %q for %s", want, id)
	return 0
}

func modelAssertReleasedAt(t *testing.T, ctx context.Context, db *sql.DB, id task.ID, resources []modelPhysicalLease, clock int64) {
	t.Helper()
	for _, r := range resources {
		var state int
		var endClock int64
		err := db.QueryRowContext(ctx,
			`SELECT state, end_clock FROM resource_leases
			 WHERE task_id = ? AND resource_type = ? AND resource_key = ?`,
			id, r.typ, r.key).Scan(&state, &endClock)
		if err != nil {
			t.Fatalf("lease row for %s %s: %v", r.typ, r.key, err)
		}
		if state != int(ledger.LeaseReleased) {
			t.Fatalf("lease %s %s state = %d; want released", r.typ, r.key, state)
		}
		if endClock != clock {
			t.Fatalf("lease %s %s end_clock = %d; want terminal clock %d", r.typ, r.key, endClock, clock)
		}
	}
}
