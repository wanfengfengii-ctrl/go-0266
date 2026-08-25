package store

import (
	"context"
	"errors"
	"testing"

	"potatoeye-cutseed-sprout-gate/ledger"
	"potatoeye-cutseed-sprout-gate/task"
)

func TestModel_SampleSealRequiresLockedBlindCodeSet(t *testing.T) {
	ctx := context.Background()
	type physicalResource struct {
		rt  ledger.ResourceType
		key ledger.ResourceKey
	}
	physicalResources := func(req LockRequest) []physicalResource {
		resources := []physicalResource{
			{rt: ledger.ResourceHealingShed, key: ledger.ResourceKey(req.HealingShed)},
			{rt: ledger.ResourceProbeWindow, key: ledger.ResourceKey(req.ProbeWindow)},
			{rt: ledger.ResourceSproutSlot, key: ledger.ResourceKey(req.SproutSlot)},
		}
		for _, well := range req.TestWells {
			resources = append(resources, physicalResource{rt: ledger.ResourceTestWell, key: ledger.ResourceKey(well)})
		}
		return resources
	}

	cases := []struct {
		name         string
		samples      []SampleBinding
		wantErr      Code
		wantBindings map[string]string
	}{
		{
			name: "accepts locked blind set regardless of submit order",
			samples: []SampleBinding{
				{BasketSeal: "seal-2", BlindCode: "code-2"},
				{BasketSeal: "seal-1", BlindCode: "code-1"},
			},
			wantBindings: map[string]string{"seal-1": "code-1", "seal-2": "code-2"},
		},
		{
			name: "rejects replacement blind code while seals match",
			samples: []SampleBinding{
				{BasketSeal: "seal-1", BlindCode: "code-1"},
				{BasketSeal: "seal-2", BlindCode: "code-x"},
			},
			wantErr: CodeConflict,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newMemStore(t)
			req := defaultLockRequest()
			id, gen := advanceToSealing(t, s, req)
			before, err := s.GetTask(ctx, id)
			if err != nil {
				t.Fatalf("get task before seal: %v", err)
			}
			if before.Task.State != task.StateSealingSamples || before.Task.Generation != gen {
				t.Fatalf("precondition state/gen = %s/%d; want sealing_samples/%d", before.Task.State, before.Task.Generation, gen)
			}
			beforeAudit, err := s.Audit(ctx, id)
			if err != nil {
				t.Fatalf("audit before seal: %v", err)
			}

			res, err := s.SampleSeal(ctx, id, SampleSealRequest{Generation: gen, Samples: tc.samples})
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("sample seal: %v", err)
				}
				if res.Sealed != len(tc.samples) {
					t.Fatalf("sealed = %d; want %d", res.Sealed, len(tc.samples))
				}
				samples, err := loadBasketSamples(ctx, s.db, id)
				if err != nil {
					t.Fatalf("load samples: %v", err)
				}
				if len(samples) != len(tc.wantBindings) {
					t.Fatalf("sample rows = %d; want %d", len(samples), len(tc.wantBindings))
				}
				for _, got := range samples {
					wantCode, ok := tc.wantBindings[got.BasketSeal]
					if !ok || got.BlindCode != wantCode {
						t.Fatalf("binding %q -> %q; want %q", got.BasketSeal, got.BlindCode, wantCode)
					}
					if !got.Sealed || got.Revealed || got.Generation != gen+1 {
						t.Fatalf("sample row = %+v; want sealed, unrevealed, generation %d", got, gen+1)
					}
				}
				for _, resource := range physicalResources(req) {
					owner, held := s.Held(resource.rt, resource.key)
					if !held || owner != id {
						t.Fatalf("physical resource %s:%s owner/held = %q/%v; want %q/true", resource.rt, resource.key, owner, held, id)
					}
				}
				after, err := s.GetTask(ctx, id)
				if err != nil {
					t.Fatalf("get task after seal: %v", err)
				}
				if after.Task.State != task.StateOccupyingShed || after.Task.Generation != gen+1 {
					t.Fatalf("state/gen = %s/%d; want occupying_shed/%d", after.Task.State, after.Task.Generation, gen+1)
				}
				afterAudit, err := s.Audit(ctx, id)
				if err != nil {
					t.Fatalf("audit after seal: %v", err)
				}
				if len(afterAudit) != len(beforeAudit)+1 {
					t.Fatalf("audit events = %d; want %d", len(afterAudit), len(beforeAudit)+1)
				}
				return
			}

			if err == nil {
				t.Fatalf("sample seal accepted bindings %+v; want %s", tc.samples, tc.wantErr)
			}
			var se *Error
			if !errors.As(err, &se) || se.Code != tc.wantErr {
				t.Fatalf("err = %v; want %s", err, tc.wantErr)
			}
			after, err := s.GetTask(ctx, id)
			if err != nil {
				t.Fatalf("get task after rejected seal: %v", err)
			}
			if after.Task.State != before.Task.State || after.Task.Generation != before.Task.Generation {
				t.Fatalf("state/gen changed from %s/%d to %s/%d", before.Task.State, before.Task.Generation, after.Task.State, after.Task.Generation)
			}
			samples, err := loadBasketSamples(ctx, s.db, id)
			if err != nil {
				t.Fatalf("load samples after rejected seal: %v", err)
			}
			if len(samples) != 0 {
				t.Fatalf("rejected seal wrote %d basket samples; want 0", len(samples))
			}
			for _, resource := range physicalResources(req) {
				if owner, held := s.Held(resource.rt, resource.key); held {
					t.Fatalf("rejected seal held physical resource %s:%s for %q", resource.rt, resource.key, owner)
				}
			}
			afterAudit, err := s.Audit(ctx, id)
			if err != nil {
				t.Fatalf("audit after rejected seal: %v", err)
			}
			if len(afterAudit) != len(beforeAudit) {
				t.Fatalf("rejected seal added audit event: got %d, want %d", len(afterAudit), len(beforeAudit))
			}
			if _, err := s.AddDiseaseEvidence(ctx, id, DiseaseRequest{Generation: gen, BlindCode: "code-x", WellID: "well-1"}); err == nil {
				t.Fatal("disease evidence for replacement blind code must be rejected")
			}
			disease, err := loadDisease(ctx, s.db, id)
			if err != nil {
				t.Fatalf("load disease after replacement blind code: %v", err)
			}
			if len(disease) != 0 {
				t.Fatalf("replacement blind code wrote %d disease rows; want 0", len(disease))
			}
		})
	}
}
