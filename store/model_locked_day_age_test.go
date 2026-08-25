package store

import (
	"context"
	"errors"
	"testing"

	"potatoeye-cutseed-sprout-gate/arbiter"
	"potatoeye-cutseed-sprout-gate/catalog"
	"potatoeye-cutseed-sprout-gate/evidence"
	"potatoeye-cutseed-sprout-gate/fixed"
)

func TestModel_LockedDayAgesGateFinalClosure(t *testing.T) {
	ctx := context.Background()

	wantStoreCode := func(t *testing.T, err error, code Code) {
		t.Helper()
		var se *Error
		if !errors.As(err, &se) || se.Code != code {
			t.Fatalf("err = %v; want %s", err, code)
		}
	}

	cases := []struct {
		name           string
		observeDays    []int
		misroutedDay   int
		commitExtraDay int
		wantFinalCode  Code
		wantReady      bool
	}{
		{
			name:           "misrouted extra age cannot replace missing locked age",
			observeDays:    []int{1, 2},
			misroutedDay:   99,
			commitExtraDay: 99,
			wantFinalCode:  CodeNotFinalizable,
		},
		{
			name:        "locked age coverage still reaches ready credential",
			observeDays: []int{1, 2, 3},
			wantReady:   true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newMemStore(t)
			id, gen := advanceToObserve(t, s)

			for _, day := range tc.observeDays {
				if _, err := s.Observe(ctx, id, evidence.AgeDay(day), observeReq(gen, observeCells())); err != nil {
					t.Fatalf("observe day %d: %v", day, err)
				}
				gen = currentGen(t, s, id)
			}

			extraCommitted := false
			if tc.misroutedDay != 0 {
				_, err := s.Observe(ctx, id, evidence.AgeDay(tc.misroutedDay), observeReq(gen, observeCells()))
				if err == nil {
					t.Errorf("observe day %d succeeded; want %s", tc.misroutedDay, CodeInvalidRequest)
					extraCommitted = true
				} else {
					wantStoreCode(t, err, CodeInvalidRequest)
				}
				gen = currentGen(t, s, id)
			}

			if tc.commitExtraDay != 0 && !extraCommitted {
				for _, c := range observeCells() {
					err := s.AppendObservation(evidence.Observation{
						TaskID:            id,
						AgeDay:            evidence.AgeDay(tc.commitExtraDay),
						BasketSeal:        evidence.BasketSeal(c.BasketSeal),
						PointID:           evidence.PointID(c.PointID),
						SuberizationGrade: c.SuberizationGrade,
						WaterLossBlocks:   c.WaterLossBlocks,
						RotBlocks:         c.RotBlocks,
						SuberizedBlocks:   c.SuberizedBlocks,
						TotalBlocks:       c.TotalBlocks,
					})
					if err != nil {
						t.Fatalf("append extra observation day %d: %v", tc.commitExtraDay, err)
					}
				}
			}

			if _, err := s.VerifyBudEyes(ctx, id, BudEyeRequest{Generation: gen, Counts: budCounts()}); err != nil {
				t.Fatalf("bud eyes: %v", err)
			}
			gen = currentGen(t, s, id)

			if _, err := s.AddDiseaseEvidence(ctx, id, DiseaseRequest{
				Generation:     gen,
				BlindCode:      "code-1",
				WellID:         "well-1",
				LesionDiameter: fixed.New(0),
				ExudateLevel:   0,
				CtValue:        fixed.New(40000),
			}); err != nil {
				t.Fatalf("disease evidence: %v", err)
			}
			gen = currentGen(t, s, id)

			for _, p := range []catalog.PersonnelID{"reviewer-1", "reviewer-2"} {
				if _, err := s.Review(ctx, id, ReviewRequest{
					PersonnelID: p,
					Generation:  gen,
					Decision:    arbiter.VerdictReadyToSprout,
				}); err != nil {
					t.Fatalf("review %s: %v", p, err)
				}
				gen = currentGen(t, s, id)
			}

			res, err := s.FinalizeTask(ctx, id, FinalizeRequest{Generation: gen})
			if tc.wantReady {
				if err != nil {
					t.Fatalf("finalize: %v", err)
				}
				if res.Verdict != arbiter.VerdictReadyToSprout {
					t.Fatalf("verdict = %s; want %s", res.Verdict, arbiter.VerdictReadyToSprout)
				}
				if res.Credential == "" {
					t.Fatal("ready verdict must carry credential")
				}
				return
			}

			if err == nil {
				t.Fatalf("finalize succeeded with verdict %s and credential %q; want %s", res.Verdict, res.Credential, tc.wantFinalCode)
			}
			wantStoreCode(t, err, tc.wantFinalCode)
		})
	}
}
