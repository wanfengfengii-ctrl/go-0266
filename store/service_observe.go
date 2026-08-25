package store

import (
	"context"
	"database/sql"
	"errors"
	"strconv"

	"potatoeye-cutseed-sprout-gate/evidence"
	"potatoeye-cutseed-sprout-gate/task"
)

// Observe submits one healing day-age coverage matrix plus probe and scale
// readings (business flow 4). A rejected probe or scale reading produces only a
// retry-pending adapter call and never a valid coverage cell.
func (s *SQLite) Observe(ctx context.Context, id task.ID, ageDay evidence.AgeDay, req ObserveRequest) (ObserveResult, error) {
	var result ObserveResult
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ObserveResult{}, err
	}
	defer tx.Rollback()

	row, err := getTaskRow(ctx, tx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ObserveResult{}, errNotFound("task not found")
		}
		return ObserveResult{}, err
	}
	if rec, ok, err := getIdempotency(ctx, tx, req.OperationID); err != nil {
		return ObserveResult{}, err
	} else if ok {
		if rec.RequestHash == hashRequest(req) {
			result = ObserveResult{Cells: len(req.Cells)}
			if err := tx.Commit(); err != nil {
				return ObserveResult{}, err
			}
			return result, nil
		}
		return ObserveResult{}, errConflict("operation id reused with different content")
	}
	if err := task.GuardGeneration(row.Generation, req.Generation); err != nil {
		return ObserveResult{}, errStale(err.Error())
	}
	if row.State != task.StateOccupyingShed && row.State != task.StateHealingObserving {
		if row.State.Terminal() || hasFinalDecision(ctx, tx, id) {
			return ObserveResult{}, errTerminal("task already finalized")
		}
		return ObserveResult{}, errConflict("observation not allowed in state " + row.State.String())
	}

	if !dayLocked(int(ageDay), row.ObserveDays) {
		return ObserveResult{}, errInvalid("age day not in locked day-age set", strconv.Itoa(int(ageDay)))
	}

	cells, err := validateObserveCells(ctx, tx, id, row, ageDay, req)
	if err != nil {
		return ObserveResult{}, err
	}

	clock, err := s.nextClock(ctx, tx)
	if err != nil {
		return ObserveResult{}, err
	}
	// Probe (shed temp + humidity) then scale (weight change) reads.
	if fault := s.adapterFault(adapterProbe, row.ProbeWindow); fault != nil {
		if _, _, err := insertAdapterCall(ctx, tx, id, adapterProbe, row.ProbeWindow, adapterStatus(fault), clock); err != nil {
			return ObserveResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return ObserveResult{}, err
		}
		return ObserveResult{}, errAdapter("probe read failed", fault.Error())
	}
	if fault := s.adapterFault(adapterScale, string(row.Batch)); fault != nil {
		if _, _, err := insertAdapterCall(ctx, tx, id, adapterScale, string(row.Batch), adapterStatus(fault), clock); err != nil {
			return ObserveResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return ObserveResult{}, err
		}
		return ObserveResult{}, errAdapter("scale read failed", fault.Error())
	}
	if _, _, err := insertAdapterCall(ctx, tx, id, adapterProbe, row.ProbeWindow, "ok", clock); err != nil {
		return ObserveResult{}, err
	}
	if _, _, err := insertAdapterCall(ctx, tx, id, adapterScale, string(row.Batch), "ok", clock); err != nil {
		return ObserveResult{}, err
	}

	version, err := nextObservationVersion(ctx, tx, id)
	if err != nil {
		return ObserveResult{}, err
	}
	for _, o := range cells {
		o.Version = version
		if err := appendObservationTx(ctx, tx, o); err != nil {
			if errors.Is(err, evidence.ErrDuplicateCoverage) {
				return ObserveResult{}, errDuplicate(err.Error())
			}
			return ObserveResult{}, err
		}
	}

	if row.State == task.StateOccupyingShed {
		if err := updateTaskState(ctx, tx, id, task.StateHealingObserving, row.Generation+1, ""); err != nil {
			return ObserveResult{}, err
		}
	}
	if err := insertAudit(ctx, tx, id, clock, "observation age "+strconv.Itoa(int(ageDay))); err != nil {
		return ObserveResult{}, err
	}
	if err := recordIdempotency(ctx, tx, req.OperationID, id, row.Generation, req,
		ObserveResult{Cells: len(req.Cells)}, clock); err != nil {
		return ObserveResult{}, err
	}

	result = ObserveResult{Cells: len(req.Cells)}
	if err := tx.Commit(); err != nil {
		return ObserveResult{}, err
	}
	return result, nil
}

// validateObserveCells builds the observation cells after enforcing seal
// membership, per-cell conservation, duplicate rejection and full coverage.
func validateObserveCells(ctx context.Context, tx *sql.Tx, id task.ID, row TaskRow, ageDay evidence.AgeDay, req ObserveRequest) ([]evidence.Observation, error) {
	seals, err := lockedSeals(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	points := make([]evidence.PointID, len(row.Points))
	for i, p := range row.Points {
		points[i] = evidence.PointID(p)
	}
	sealSet := make(map[string]bool, len(seals))
	for _, s := range seals {
		sealSet[s] = true
	}

	cells := make([]evidence.Observation, 0, len(req.Cells))
	seen := make(map[string]bool, len(req.Cells))
	for _, c := range req.Cells {
		if !sealSet[c.BasketSeal] {
			return nil, errInvalid("unknown basket seal", c.BasketSeal)
		}
		o := evidence.Observation{
			TaskID:            id,
			AgeDay:            ageDay,
			BasketSeal:        evidence.BasketSeal(c.BasketSeal),
			PointID:           evidence.PointID(c.PointID),
			SuberizationGrade: c.SuberizationGrade,
			WaterLossBlocks:   c.WaterLossBlocks,
			RotBlocks:         c.RotBlocks,
			SuberizedBlocks:   c.SuberizedBlocks,
			TotalBlocks:       c.TotalBlocks,
		}
		if err := evidence.ValidateCell(o); err != nil {
			return nil, errInvalid(err.Error())
		}
		key := string(o.BasketSeal) + "\x00" + string(o.PointID)
		if seen[key] {
			return nil, errDuplicate("duplicate coverage cell", string(o.BasketSeal)+" "+string(o.PointID))
		}
		seen[key] = true
		cells = append(cells, o)
	}
	if err := evidence.CoverageComplete(ageDay, cells, sealIDs(seals), points); err != nil {
		return nil, errInvalid(err.Error())
	}
	if err := evidence.ValidateShedTemp(req.ShedTemp); err != nil {
		return nil, errInvalid(err.Error())
	}
	if err := evidence.ValidateHumidity(req.Humidity); err != nil {
		return nil, errInvalid(err.Error())
	}
	if err := evidence.ValidateWeightChange(req.WeightChange); err != nil {
		return nil, errInvalid(err.Error())
	}
	return cells, nil
}

// VerifyBudEyes records bud-eye active counts per coverage point (business flow
// 5), enforcing the closed-interval bound for every point.
func (s *SQLite) VerifyBudEyes(ctx context.Context, id task.ID, req BudEyeRequest) (BudEyeResult, error) {
	var result BudEyeResult
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
				result = BudEyeResult{Points: len(req.Counts)}
				return nil
			}
			return errConflict("operation id reused with different content")
		}
		if err := task.GuardGeneration(row.Generation, req.Generation); err != nil {
			return errStale(err.Error())
		}
		if row.State != task.StateHealingObserving && row.State != task.StateBudEyeVerifying {
			if row.State.Terminal() || hasFinalDecision(ctx, tx, id) {
				return errTerminal("task already finalized")
			}
			return errConflict("bud-eye verification not allowed in state " + row.State.String())
		}

		for _, c := range req.Counts {
			if err := evidence.ValidateBudEyeCount(evidence.BasketSeal(c.BasketSeal), evidence.PointID(c.PointID), c.Active, c.Capacity); err != nil {
				return errInvalid(err.Error())
			}
			if err := updateBudEyeTx(ctx, tx, id, evidence.BasketSeal(c.BasketSeal), evidence.PointID(c.PointID), c.Active, c.Capacity); err != nil {
				return errInvalid(err.Error())
			}
		}
		clock, err := s.nextClock(ctx, tx)
		if err != nil {
			return err
		}
		if row.State == task.StateHealingObserving {
			if err := updateTaskState(ctx, tx, id, task.StateBudEyeVerifying, row.Generation+1, ""); err != nil {
				return err
			}
		}
		if err := insertAudit(ctx, tx, id, clock, "bud-eye verified"); err != nil {
			return err
		}
		if err := recordIdempotency(ctx, tx, req.OperationID, id, row.Generation, req,
			BudEyeResult{Points: len(req.Counts)}, clock); err != nil {
			return err
		}
		result = BudEyeResult{Points: len(req.Counts)}
		return nil
	})
	if err != nil {
		return BudEyeResult{}, err
	}
	return result, nil
}

func lockedSeals(ctx context.Context, q dbtx, id task.ID) ([]string, error) {
	samples, err := loadBasketSamples(ctx, q, id)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(samples))
	for i, s := range samples {
		out[i] = s.BasketSeal
	}
	return out, nil
}

func sealIDs(seals []string) []evidence.BasketSeal {
	out := make([]evidence.BasketSeal, len(seals))
	for i, s := range seals {
		out[i] = evidence.BasketSeal(s)
	}
	return out
}

// dayLocked reports whether the submitted age_day is one of the locked
// observation day-ages. Only locked day-ages may form valid coverage so that an
// observation committed under an out-of-lock day can never mask a missing
// locked day at the final barrier (domain rule 5 / acceptance 8).
func dayLocked(day int, lockedDays []int) bool {
	for _, d := range lockedDays {
		if d == day {
			return true
		}
	}
	return false
}
