package store

import (
	"context"
	"errors"
	"strconv"

	"potatoeye-cutseed-sprout-gate/evidence"
	"potatoeye-cutseed-sprout-gate/ledger"
	"potatoeye-cutseed-sprout-gate/task"
)

// AddDiseaseEvidence records a rapid-test plate-well reading on the immutable
// evidence version chain (business flow 5). The blind code must index a sealed
// sample, and a rejected/malformed plate reader produces only a retry call.
func (s *SQLite) AddDiseaseEvidence(ctx context.Context, id task.ID, req DiseaseRequest) (DiseaseResult, error) {
	var result DiseaseResult
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return DiseaseResult{}, err
	}
	defer tx.Rollback()

	row, err := getTaskRow(ctx, tx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return DiseaseResult{}, errNotFound("task not found")
		}
		return DiseaseResult{}, err
	}
	if rec, ok, err := getIdempotency(ctx, tx, req.OperationID); err != nil {
		return DiseaseResult{}, err
	} else if ok {
		if rec.RequestHash == hashRequest(req) {
			v, err := nextDiseaseVersion(ctx, tx, id)
			if err != nil {
				return DiseaseResult{}, err
			}
			result = DiseaseResult{Version: v - 1}
			if err := tx.Commit(); err != nil {
				return DiseaseResult{}, err
			}
			return result, nil
		}
		return DiseaseResult{}, errConflict("operation id reused with different content")
	}
	if err := task.GuardGeneration(row.Generation, req.Generation); err != nil {
		return DiseaseResult{}, errStale(err.Error())
	}
	if row.State != task.StateBudEyeVerifying && row.State != task.StateDiseaseRetesting {
		if row.State.Terminal() || hasFinalDecision(ctx, tx, id) {
			return DiseaseResult{}, errTerminal("task already finalized")
		}
		return DiseaseResult{}, errConflict("disease evidence not allowed in state " + row.State.String())
	}

	// The blind code is only a sample index before reveal; it must map to a
	// sealed sample of this task (domain rule 4).
	if _, err := findBlindCode(ctx, tx, id, req.BlindCode); err != nil {
		if errors.Is(err, ledger.ErrBlindMismatch) {
			return DiseaseResult{}, errInvalid("unknown blind code", req.BlindCode)
		}
		return DiseaseResult{}, err
	}

	d := evidence.DiseaseEvidence{
		TaskID:         id,
		BlindCode:      req.BlindCode,
		WellID:         evidence.WellID(req.WellID),
		LesionDiameter: req.LesionDiameter,
		ExudateLevel:   req.ExudateLevel,
		CtValue:        req.CtValue,
		RejudgeGen:     row.Generation,
	}
	if err := evidence.ValidateDisease(d); err != nil {
		return DiseaseResult{}, errInvalid(err.Error())
	}

	clock, err := s.nextClock(ctx, tx)
	if err != nil {
		return DiseaseResult{}, err
	}
	if fault := s.adapterFault(adapterPlate, req.WellID); fault != nil {
		if _, _, err := insertAdapterCall(ctx, tx, id, adapterPlate, req.WellID, adapterStatus(fault), clock); err != nil {
			return DiseaseResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return DiseaseResult{}, err
		}
		return DiseaseResult{}, errAdapter("plate reader failed", fault.Error())
	}
	callID, _, err := insertAdapterCall(ctx, tx, id, adapterPlate, req.WellID, "ok", clock)
	if err != nil {
		return DiseaseResult{}, err
	}
	d.SourceCallID = callID

	version, err := nextDiseaseVersion(ctx, tx, id)
	if err != nil {
		return DiseaseResult{}, err
	}
	d.Version = version
	if err := appendDiseaseTx(ctx, tx, d); err != nil {
		return DiseaseResult{}, err
	}

	if row.State == task.StateBudEyeVerifying {
		if err := updateTaskState(ctx, tx, id, task.StateDiseaseRetesting, row.Generation+1, ""); err != nil {
			return DiseaseResult{}, err
		}
	}
	if err := insertAudit(ctx, tx, id, clock, "disease evidence version "+strconv.Itoa(int(version))); err != nil {
		return DiseaseResult{}, err
	}
	if err := recordIdempotency(ctx, tx, req.OperationID, id, row.Generation, req,
		DiseaseResult{Version: version, AdapterCall: callID}, clock); err != nil {
		return DiseaseResult{}, err
	}

	result = DiseaseResult{Version: version, AdapterCall: callID}
	if err := tx.Commit(); err != nil {
		return DiseaseResult{}, err
	}
	return result, nil
}
