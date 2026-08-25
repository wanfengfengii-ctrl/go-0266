package store

import (
	"context"
	"database/sql"
	"errors"

	"potatoeye-cutseed-sprout-gate/arbiter"
	"potatoeye-cutseed-sprout-gate/catalog"
	"potatoeye-cutseed-sprout-gate/task"
)

// Review records one independent reviewer's decision (business flow 6). The
// reviewer must be qualified, distinct from the cutters and distinct from other
// reviewers (role separation, acceptance 3).
func (s *SQLite) Review(ctx context.Context, id task.ID, req ReviewRequest) (ReviewResult, error) {
	var result ReviewResult
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
				reviews, err := loadReviews(ctx, tx, id)
				if err != nil {
					return err
				}
				result = ReviewResult{Reviews: len(reviews)}
				return nil
			}
			return errConflict("operation id reused with different content")
		}
		if err := task.GuardGeneration(row.Generation, req.Generation); err != nil {
			return errStale(err.Error())
		}
		if row.State != task.StateDiseaseRetesting && row.State != task.StatePendingReview {
			if row.State.Terminal() || hasFinalDecision(ctx, tx, id) {
				return errTerminal("task already finalized")
			}
			return errConflict("review not allowed in state " + row.State.String())
		}
		if !req.Decision.Valid() {
			return errInvalid("invalid review decision", string(req.Decision))
		}

		existing, err := loadReviews(ctx, tx, id)
		if err != nil {
			return err
		}
		cutters, err := loadCutConfirmers(ctx, tx, id)
		if err != nil {
			return err
		}
		review := arbiter.Review{
			TaskID:      id,
			PersonnelID: string(req.PersonnelID),
			Decision:    req.Decision,
			Generation:  req.Generation,
		}
		// Role separation is checked before qualification so a cutter on the
		// same task is rejected as an overlap regardless of other roles.
		if err := arbiter.ValidateReview(review, existing, personnelStrings(cutters)); err != nil {
			if errors.Is(err, arbiter.ErrRoleOverlap) {
				return errConflict("reviewer overlaps with cutter", err.Error())
			}
			return errDuplicate("duplicate reviewer", err.Error())
		}

		rule, err := s.Catalog().Match(row.Plot, row.Variety)
		if err != nil {
			return errInvalid("rule missing", err.Error())
		}
		if !s.Catalog().Qualified(req.PersonnelID, catalog.RoleReviewer, rule.Version) {
			return errInvalid("reviewer not qualified", string(req.PersonnelID))
		}

		if err := insertReview(ctx, tx, review); err != nil {
			return err
		}
		clock, err := s.nextClock(ctx, tx)
		if err != nil {
			return err
		}
		if row.State == task.StateDiseaseRetesting {
			if err := updateTaskState(ctx, tx, id, task.StatePendingReview, row.Generation+1, ""); err != nil {
				return err
			}
		}
		if err := insertAudit(ctx, tx, id, clock, "review by "+string(req.PersonnelID)); err != nil {
			return err
		}
		if err := recordIdempotency(ctx, tx, req.OperationID, id, row.Generation, req,
			ReviewResult{Reviews: len(existing) + 1}, clock); err != nil {
			return err
		}
		result = ReviewResult{Reviews: len(existing) + 1}
		return nil
	})
	if err != nil {
		return ReviewResult{}, err
	}
	return result, nil
}
