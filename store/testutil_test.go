package store

import (
	"context"
	"path/filepath"
	"testing"

	"potatoeye-cutseed-sprout-gate/arbiter"
	"potatoeye-cutseed-sprout-gate/catalog"
	"potatoeye-cutseed-sprout-gate/evidence"
	"potatoeye-cutseed-sprout-gate/fixed"
	"potatoeye-cutseed-sprout-gate/task"
)

func newMemStore(t *testing.T) *SQLite {
	t.Helper()
	s, err := OpenMemory()
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func newFileStore(t *testing.T) *SQLite {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open file store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func defaultLockRequest() LockRequest {
	return LockRequest{
		Plot: "P-01", Variety: "V-G2", Cellar: "C-01", Disinfect: "D-01",
		Batch:       "B-1",
		BasketSeals: []string{"seal-1", "seal-2"},
		BlindCodes:  []string{"code-1", "code-2"},
		HealingShed: "shed-1", ProbeWindow: "probe-1", SproutSlot: "slot-1",
		TestWells: []string{"well-1"},
		Points:    []string{"p-1", "p-2"},
		Reviewers: []catalog.PersonnelID{"reviewer-1", "reviewer-2"},
	}
}

func lockDefault(t *testing.T, s *SQLite) (task.ID, task.Generation) {
	t.Helper()
	res, err := s.Lock(context.Background(), defaultLockRequest())
	if err != nil {
		t.Fatalf("lock: %v", err)
	}
	return res.TaskID, res.Generation
}

func currentGen(t *testing.T, s *SQLite, id task.ID) task.Generation {
	t.Helper()
	d, err := s.GetTask(context.Background(), id)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	return d.Task.Generation
}

func currentSnapshot(t *testing.T, s *SQLite, id task.ID) task.LockSnapshot {
	t.Helper()
	d, err := s.GetTask(context.Background(), id)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	return d.Task.Snapshot
}

func observeCells() []ObserveCell {
	var cells []ObserveCell
	for _, seal := range []string{"seal-1", "seal-2"} {
		for _, point := range []string{"p-1", "p-2"} {
			cells = append(cells, ObserveCell{
				BasketSeal: seal, PointID: point,
				SuberizationGrade: 2, WaterLossBlocks: 1, RotBlocks: 1,
				SuberizedBlocks: 8, TotalBlocks: 10,
			})
		}
	}
	return cells
}

func budCounts() []BudEyeCount {
	var counts []BudEyeCount
	for _, seal := range []string{"seal-1", "seal-2"} {
		for _, point := range []string{"p-1", "p-2"} {
			counts = append(counts, BudEyeCount{BasketSeal: seal, PointID: point, Active: 3, Capacity: 10})
		}
	}
	return counts
}

// driveToDisease drives a freshly locked task through cut confirmation, sample
// seal, full coverage, bud-eye verification and the given disease reading,
// leaving it in disease_retesting (before reviews). The disease request's
// generation is filled in automatically.
func driveToDisease(t *testing.T, s *SQLite, id task.ID, diseaseReq DiseaseRequest) {
	t.Helper()
	ctx := context.Background()
	gen := currentGen(t, s, id)
	snap := currentSnapshot(t, s, id)

	for _, p := range []catalog.PersonnelID{"cutter-1", "cutter-2"} {
		if _, err := s.CutConfirm(ctx, id, CutConfirmRequest{PersonnelID: p, Generation: gen, Snapshot: snap}); err != nil {
			t.Fatalf("cut confirm %s: %v", p, err)
		}
		gen = currentGen(t, s, id)
	}
	if _, err := s.SampleSeal(ctx, id, SampleSealRequest{
		Generation: gen,
		Samples:    []SampleBinding{{BasketSeal: "seal-1", BlindCode: "code-1"}, {BasketSeal: "seal-2", BlindCode: "code-2"}},
	}); err != nil {
		t.Fatalf("sample seal: %v", err)
	}
	gen = currentGen(t, s, id)

	for _, day := range []int{1, 2, 3} {
		if _, err := s.Observe(ctx, id, evidence.AgeDay(day), ObserveRequest{
			Generation: gen, Cells: observeCells(),
			ShedTemp: fixed.New(20000), Humidity: fixed.New(60000), WeightChange: fixed.New(1000),
		}); err != nil {
			t.Fatalf("observe day %d: %v", day, err)
		}
		gen = currentGen(t, s, id)
	}

	if _, err := s.VerifyBudEyes(ctx, id, BudEyeRequest{Generation: gen, Counts: budCounts()}); err != nil {
		t.Fatalf("bud eyes: %v", err)
	}
	gen = currentGen(t, s, id)

	diseaseReq.Generation = gen
	if _, err := s.AddDiseaseEvidence(ctx, id, diseaseReq); err != nil {
		t.Fatalf("disease evidence: %v", err)
	}
}

// driveToDiseaseRetesting drives a freshly locked task to disease_retesting
// with a clean (negative) disease reading.
func driveToDiseaseRetesting(t *testing.T, s *SQLite, id task.ID) {
	t.Helper()
	driveToDisease(t, s, id, DiseaseRequest{
		BlindCode: "code-1", WellID: "well-1",
		LesionDiameter: fixed.New(0), ExudateLevel: 0, CtValue: fixed.New(40000),
	})
}

// happyPath drives a freshly locked task through the full closed-evidence path
// and two independent reviews, leaving the task in pending_review.
func happyPath(t *testing.T, s *SQLite, id task.ID) {
	t.Helper()
	driveToDiseaseRetesting(t, s, id)
	ctx := context.Background()
	gen := currentGen(t, s, id)
	for _, p := range []catalog.PersonnelID{"reviewer-1", "reviewer-2"} {
		if _, err := s.Review(ctx, id, ReviewRequest{
			PersonnelID: p, Generation: gen, Decision: arbiter.VerdictReadyToSprout,
		}); err != nil {
			t.Fatalf("review %s: %v", p, err)
		}
		gen = currentGen(t, s, id)
	}
}
