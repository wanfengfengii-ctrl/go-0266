package store

import (
	"context"
	"database/sql"
	"errors"
	"sort"

	"potatoeye-cutseed-sprout-gate/ledger"
	"potatoeye-cutseed-sprout-gate/task"
)

// SampleSeal binds each basket seal to a blind code and atomically occupies the
// physical healing shed, probe window, sprout slot and test wells (business
// flow 3). Any conflict rolls back the whole write.
func (s *SQLite) SampleSeal(ctx context.Context, id task.ID, req SampleSealRequest) (SampleSealResult, error) {
	var result SampleSealResult
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		row, err := getTaskRow(ctx, tx, id)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return errNotFound("task not found")
			}
			return err
		}
		if rec, ok, err := getIdempotency(ctx, tx, req.OperationID); err != nil {
			return err
		} else if ok {
			if rec.RequestHash == hashRequest(req) {
				result = SampleSealResult{Sealed: len(req.Samples)}
				return nil
			}
			return errConflict("operation id reused with different content")
		}
		if err := task.GuardGeneration(row.Generation, req.Generation); err != nil {
			return errStale(err.Error())
		}
		if row.State != task.StateSealingSamples {
			if row.State.Terminal() || hasFinalDecision(ctx, tx, id) {
				return errTerminal("task already finalized")
			}
			return errConflict("sample sealing not allowed in state " + row.State.String())
		}

		bindings := make([]ledger.SealBinding, len(req.Samples))
		for i, b := range req.Samples {
			bindings[i] = ledger.SealBinding{BasketSeal: b.BasketSeal, BlindCode: b.BlindCode}
		}
		if err := ledger.ValidateSealBinding(bindings); err != nil {
			if errors.Is(err, ledger.ErrDuplicateBlindCode) {
				return errDuplicate("duplicate blind code", err.Error())
			}
			return errConflict("duplicate seal", err.Error())
		}

		// The submitted bindings must exactly cover the locked seals and codes.
		lockedSeals, err := loadResourceKeys(ctx, tx, id, ledger.ResourceBasketSeal)
		if err != nil {
			return err
		}
		lockedCodes, err := loadResourceKeys(ctx, tx, id, ledger.ResourceBlindCode)
		if err != nil {
			return err
		}
		seals := make([]string, len(req.Samples))
		codes := make([]string, len(req.Samples))
		for i, b := range req.Samples {
			seals[i] = b.BasketSeal
			codes[i] = b.BlindCode
		}
		sort.Strings(seals)
		sort.Strings(codes)
		if !stringSlicesEqual(seals, lockedSeals) || !stringSlicesEqual(codes, lockedCodes) {
			return errConflict("sample seals do not match locked set")
		}

		clock, err := s.nextClock(ctx, tx)
		if err != nil {
			return err
		}
		physical := ledger.PhysicalLeases(row.HealingShed, row.ProbeWindow, row.SproutSlot, row.TestWells)
		if _, err := acquireLeases(ctx, tx, id, row.Generation+1, physical, clock); err != nil {
			return err
		}
		for _, b := range req.Samples {
			sample := ledger.BasketSample{
				TaskID: id, BasketSeal: b.BasketSeal, BlindCode: b.BlindCode,
				Sealed: true, Generation: row.Generation + 1,
			}
			if err := insertBasketSample(ctx, tx, sample); err != nil {
				return err
			}
		}
		if err := updateTaskState(ctx, tx, id, task.StateOccupyingShed, row.Generation+1, ""); err != nil {
			return err
		}
		if err := insertAudit(ctx, tx, id, clock, "samples sealed and resources occupied"); err != nil {
			return err
		}
		if err := recordIdempotency(ctx, tx, req.OperationID, id, row.Generation+1, req,
			SampleSealResult{Sealed: len(req.Samples)}, clock); err != nil {
			return err
		}
		result = SampleSealResult{Sealed: len(req.Samples)}
		return nil
	})
	if err != nil {
		return SampleSealResult{}, err
	}
	return result, nil
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
