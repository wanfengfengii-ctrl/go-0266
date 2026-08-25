package store

import (
	"context"
	"database/sql"
	"errors"

	"potatoeye-cutseed-sprout-gate/catalog"
	"potatoeye-cutseed-sprout-gate/task"
)

// CutConfirm records one cutter's confirmation of the frozen snapshot (business
// flow 2). Two distinct qualified cutters must confirm before sealing begins.
func (s *SQLite) CutConfirm(ctx context.Context, id task.ID, req CutConfirmRequest) (CutConfirmResult, error) {
	var result CutConfirmResult
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
				return s.replayConfirm(ctx, tx, id, &result)
			}
			return errConflict("operation id reused with different content")
		}

		if err := task.GuardGeneration(row.Generation, req.Generation); err != nil {
			return errStale(err.Error())
		}
		if row.State != task.StatePendingCutConfirm {
			if row.State.Terminal() || hasFinalDecision(ctx, tx, id) {
				return errTerminal("task already finalized")
			}
			return errConflict("cut confirmation not allowed in state " + row.State.String())
		}

		rule, err := s.Catalog().Match(row.Plot, row.Variety)
		if err != nil {
			return errInvalid("rule missing", err.Error())
		}
		if !s.Catalog().Qualified(req.PersonnelID, catalog.RoleCutter, rule.Version) {
			return errInvalid("cutter not qualified", string(req.PersonnelID))
		}

		confirmed, err := loadCutConfirmers(ctx, tx, id)
		if err != nil {
			return err
		}
		for _, p := range confirmed {
			if p == req.PersonnelID {
				return errDuplicate("duplicate cutter", task.ErrDuplicateCutter.Error())
			}
		}

		if err := insertCutConfirmation(ctx, tx, id, req.PersonnelID); err != nil {
			return err
		}
		clock, err := s.nextClock(ctx, tx)
		if err != nil {
			return err
		}
		if err := insertAudit(ctx, tx, id, clock, "cut confirmed by "+string(req.PersonnelID)); err != nil {
			return err
		}

		confirmed = append(confirmed, req.PersonnelID)
		complete := len(confirmed) >= 2
		newGen := row.Generation
		newState := row.State
		if complete {
			newState = task.StateSealingSamples
			newGen++
			if err := updateTaskState(ctx, tx, id, newState, newGen, ""); err != nil {
				return err
			}
		}

		if err := recordIdempotency(ctx, tx, req.OperationID, id, newGen, req, CutConfirmResult{
			Confirmed: confirmed, Complete: complete,
		}, clock); err != nil {
			return err
		}

		result = CutConfirmResult{Confirmed: confirmed, Complete: complete}
		return nil
	})
	if err != nil {
		return CutConfirmResult{}, err
	}
	return result, nil
}

func (s *SQLite) replayConfirm(ctx context.Context, tx *sql.Tx, id task.ID, result *CutConfirmResult) error {
	confirmed, err := loadCutConfirmers(ctx, tx, id)
	if err != nil {
		return err
	}
	result.Confirmed = confirmed
	result.Complete = len(confirmed) >= 2
	return nil
}

func personnelStrings(v []catalog.PersonnelID) []string {
	out := make([]string, len(v))
	for i, p := range v {
		out[i] = string(p)
	}
	return out
}
