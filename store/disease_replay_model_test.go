package store

import (
	"context"
	"errors"
	"testing"

	"potatoeye-cutseed-sprout-gate/fixed"
	"potatoeye-cutseed-sprout-gate/task"
)

func TestModel_DiseaseEvidenceReplayPreservesFirstResponse(t *testing.T) {
	ctx := context.Background()

	setupBudEyeVerified := func(t *testing.T) (*SQLite, task.ID, task.Generation) {
		t.Helper()
		s := newMemStore(t)
		id, gen := advanceToObserve(t, s)
		if _, err := s.Observe(ctx, id, 1, observeReq(gen, observeCells())); err != nil {
			t.Fatalf("observe: %v", err)
		}
		gen = currentGen(t, s, id)
		if _, err := s.VerifyBudEyes(ctx, id, BudEyeRequest{Generation: gen, Counts: budCounts()}); err != nil {
			t.Fatalf("bud eyes: %v", err)
		}
		return s, id, currentGen(t, s, id)
	}

	countPlateCalls := func(t *testing.T, s *SQLite, id task.ID) map[string]AdapterCall {
		t.Helper()
		calls, err := loadAdapterCalls(ctx, s.db, id)
		if err != nil {
			t.Fatalf("load adapter calls: %v", err)
		}
		plate := make(map[string]AdapterCall)
		for _, c := range calls {
			if c.Adapter == adapterPlate {
				plate[c.CallID] = c
			}
		}
		return plate
	}

	cases := []struct {
		name          string
		replay        bool
		fault         error
		wantStatus    string
		wantRetryCode Code
	}{
		{name: "lost first response replay ignores later evidence append", replay: true},
		{name: "malformed plate reading writes retry call only", fault: ErrAdapterMalformed, wantStatus: "malformed", wantRetryCode: CodeAdapterRetry},
		{name: "disconnected plate reading writes retry call only", fault: ErrAdapterDisconnected, wantStatus: "disconnected", wantRetryCode: CodeAdapterRetry},
		{name: "timeout plate reading writes retry call only", fault: ErrAdapterTimeout, wantStatus: "timeout", wantRetryCode: CodeAdapterRetry},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, id, gen := setupBudEyeVerified(t)

			if tc.replay {
				firstReq := DiseaseRequest{
					OperationID:    "disease-op-first",
					Generation:     gen,
					BlindCode:      "code-1",
					WellID:         "well-1",
					LesionDiameter: fixed.New(12000),
					ExudateLevel:   1,
					CtValue:        fixed.New(32000),
				}
				first, err := s.AddDiseaseEvidence(ctx, id, firstReq)
				if err != nil {
					t.Fatalf("first disease evidence: %v", err)
				}
				if first.Version != 1 || first.AdapterCall == "" {
					t.Fatalf("first result = %+v; want version 1 with adapter_call_id", first)
				}

				secondReq := DiseaseRequest{
					OperationID:    "disease-op-second",
					Generation:     currentGen(t, s, id),
					BlindCode:      "code-2",
					WellID:         "well-1",
					LesionDiameter: fixed.New(18000),
					ExudateLevel:   2,
					CtValue:        fixed.New(31000),
				}
				second, err := s.AddDiseaseEvidence(ctx, id, secondReq)
				if err != nil {
					t.Fatalf("second disease evidence: %v", err)
				}
				if second.Version != first.Version+1 || second.AdapterCall == "" || second.AdapterCall == first.AdapterCall {
					t.Fatalf("second result = %+v after first %+v; want next version with distinct adapter call", second, first)
				}

				replayed, err := s.AddDiseaseEvidence(ctx, id, firstReq)
				if err != nil {
					t.Fatalf("replay first operation: %v", err)
				}
				if replayed != first {
					t.Fatalf("replay result = %+v; want original first response %+v", replayed, first)
				}

				disease, err := loadDisease(ctx, s.db, id)
				if err != nil {
					t.Fatalf("load disease: %v", err)
				}
				if len(disease) != 2 {
					t.Fatalf("disease rows after replay = %d; want only the two original writes", len(disease))
				}
				plate := countPlateCalls(t, s, id)
				if len(plate) != 2 {
					t.Fatalf("plate calls after replay = %d; want no new adapter call on replay", len(plate))
				}
				if _, ok := plate[first.AdapterCall]; !ok {
					t.Fatalf("first adapter call %q missing from plate call log", first.AdapterCall)
				}
				if _, ok := plate[second.AdapterCall]; !ok {
					t.Fatalf("second adapter call %q missing from plate call log", second.AdapterCall)
				}
				return
			}

			s.InjectFault("plate_reader:well-1", tc.fault)
			_, err := s.AddDiseaseEvidence(ctx, id, DiseaseRequest{
				OperationID:    task.OperationID("disease-op-" + tc.wantStatus),
				Generation:     gen,
				BlindCode:      "code-1",
				WellID:         "well-1",
				LesionDiameter: fixed.New(1000),
				ExudateLevel:   1,
				CtValue:        fixed.New(30000),
			})
			if err == nil {
				t.Fatal("faulted plate reading must return an adapter retry error")
			}
			var se *Error
			if !errors.As(err, &se) || se.Code != tc.wantRetryCode {
				t.Fatalf("err = %v; want %s", err, tc.wantRetryCode)
			}
			disease, err := loadDisease(ctx, s.db, id)
			if err != nil {
				t.Fatalf("load disease: %v", err)
			}
			if len(disease) != 0 {
				t.Fatalf("faulted plate reading wrote %d disease rows; want 0", len(disease))
			}
			plate := countPlateCalls(t, s, id)
			if len(plate) != 1 {
				t.Fatalf("plate calls after fault = %d; want exactly one retry call", len(plate))
			}
			for _, c := range plate {
				if c.Status != tc.wantStatus {
					t.Fatalf("plate call = %+v; want status %q", c, tc.wantStatus)
				}
			}
		})
	}
}
