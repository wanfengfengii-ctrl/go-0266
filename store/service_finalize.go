package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"

	"potatoeye-cutseed-sprout-gate/arbiter"
	"potatoeye-cutseed-sprout-gate/evidence"
	"potatoeye-cutseed-sprout-gate/fixed"
	"potatoeye-cutseed-sprout-gate/task"
)

// Finalize runs the single-writer barrier that produces exactly one terminal
// verdict (business flow 7/8). A cancellation is allowed from any open state;
// a normal finalization requires the evidence to be closed and two independent
// reviews to be present.
func (s *SQLite) FinalizeTask(ctx context.Context, id task.ID, req FinalizeRequest) (FinalizeResult, error) {
	var result FinalizeResult
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
				d, ok, err := loadFinalDecision(ctx, tx, id)
				if err != nil {
					return err
				}
				if ok {
					result = FinalizeResult{Verdict: d.Verdict, Credential: d.Credential, Reason: d.Reason}
				}
				return nil
			}
			return errConflict("operation id reused with different content")
		}
		if err := task.GuardGeneration(row.Generation, req.Generation); err != nil {
			return errStale(err.Error())
		}
		if err := task.GuardOpen(row.State); err != nil {
			return errTerminal(err.Error())
		}
		if hasFinalDecision(ctx, tx, id) {
			return errTerminal("task already finalized")
		}

		verdict := arbiter.VerdictReadyToSprout
		reason := "evidence closed and independently reviewed"
		if req.Cancelled {
			verdict = arbiter.VerdictCancelled
			reason = "task cancelled"
		} else {
			if row.State != task.StatePendingReview {
				return errNotFinalizable("reviews not complete")
			}
			summary, err := s.computeSummary(ctx, tx, row)
			if err != nil {
				return err
			}
			reviews, err := loadReviews(ctx, tx, id)
			if err != nil {
				return err
			}
			v, err := arbiter.SelectVerdict(summary, len(reviews))
			if err != nil {
				return errNotFinalizable(err.Error())
			}
			verdict = v
			if verdict == arbiter.VerdictDiseaseQuarantine {
				reason = "disease threshold exceeded"
			}
		}

		clock, err := s.nextClock(ctx, tx)
		if err != nil {
			return err
		}
		credential := credentialFor(id, verdict, clock)
		decision := arbiter.FinalDecision{
			TaskID: id, Verdict: verdict, Generation: row.Generation + 1,
			Credential: credential, Reason: reason,
		}
		// Insert the final decision first so its unique primary key acts as the
		// single-writer barrier: only one concurrent finalize can commit.
		if err := insertFinalDecision(ctx, tx, decision); err != nil {
			if isUniqueViolation(err) {
				return errConflict("already finalized")
			}
			return err
		}
		terminal := stateForVerdict(verdict)
		if err := updateTaskState(ctx, tx, id, terminal, row.Generation+1, reason); err != nil {
			return err
		}
		if err := insertAudit(ctx, tx, id, clock, "finalized "+string(verdict)); err != nil {
			return err
		}
		if err := recordIdempotency(ctx, tx, req.OperationID, id, row.Generation+1, req,
			FinalizeResult{Verdict: verdict, Credential: credential, Reason: reason}, clock); err != nil {
			return err
		}
		result = FinalizeResult{Verdict: verdict, Credential: credential, Reason: reason}
		return nil
	})
	if err != nil {
		return FinalizeResult{}, err
	}
	return result, nil
}

func stateForVerdict(v arbiter.Verdict) task.State {
	switch v {
	case arbiter.VerdictReadyToSprout:
		return task.StateReadyToSprout
	case arbiter.VerdictDiseaseQuarantine:
		return task.StateDiseaseQuarantine
	case arbiter.VerdictCancelled:
		return task.StateCancelled
	default:
		return task.StateCancelled
	}
}

// computeSummary derives the evidence closure projection for the final barrier.
func (s *SQLite) computeSummary(ctx context.Context, tx *sql.Tx, row TaskRow) (arbiter.EvidenceSummary, error) {
	obs, err := loadObservations(ctx, tx, row.ID)
	if err != nil {
		return arbiter.EvidenceSummary{}, err
	}
	disease, err := loadDisease(ctx, tx, row.ID)
	if err != nil {
		return arbiter.EvidenceSummary{}, err
	}

	summary := arbiter.EvidenceSummary{SuberizationOK: true, WaterLossOK: true, BudEyeOK: true, DiseaseClean: true}
	for _, o := range obs {
		rate, err := computeWaterLossRate(o.WaterLossBlocks, o.TotalBlocks)
		if err != nil {
			return summary, err
		}
		suberOK, waterOK := arbiter.CellMeetsThresholds(o.SuberizationGrade, rate, row.snapshot().Thresholds)
		if !suberOK {
			summary.SuberizationOK = false
		}
		if !waterOK {
			summary.WaterLossOK = false
		}
	}
	for _, d := range disease {
		if arbiter.DiseaseExceedsThreshold(d.LesionDiameter, d.ExudateLevel, d.CtValue, row.snapshot().Thresholds) {
			summary.DiseaseClean = false
		}
	}

	seals, err := lockedSeals(ctx, tx, row.ID)
	if err != nil {
		return summary, err
	}
	points := row.Points
	// The final barrier may only accept full coverage of the locked day-age
	// set (domain rule 5 / acceptance 8). A cell whose age_day falls outside the
	// locked set (for example a day submitted to the wrong path) must never
	// count toward closure, even after bud-eye verification wrote a capacity
	// across every age for the (seal, point) pair.
	expected := len(row.ObserveDays) * len(seals) * len(points)
	var verified int
	if err := tx.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM observations WHERE task_id = ? AND age_day IN ("+placeholders(len(row.ObserveDays))+") AND bud_eye_capacity > 0",
		queryArgs(row.ID, row.ObserveDays)...).Scan(&verified); err != nil {
		return summary, err
	}
	summary.Complete = verified == expected && everyLockedDayCovered(row.ObserveDays, obs, seals, points)
	if verified < expected || !summary.Complete {
		summary.BudEyeOK = false
	}
	return summary, nil
}

// placeholders builds a comma-joined "?" placeholder list of n entries for an
// IN clause.
func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	out := make([]byte, 0, n*2-1)
	for i := 0; i < n; i++ {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, '?')
	}
	return string(out)
}

// queryArgs prepends id to the observe days for use as the positional args of
// the IN-bound coverage count query.
func queryArgs(id task.ID, days []int) []any {
	args := make([]any, 0, 1+len(days))
	args = append(args, id)
	for _, d := range days {
		args = append(args, d)
	}
	return args
}

// everyLockedDayCovered reports whether every locked day-age carries a full
// (seal, point) coverage matrix in the committed observations. This is the
// strict per-day closure the final barrier relies on so that a missing locked
// day can never be masked by cells committed under an out-of-lock day-age.
func everyLockedDayCovered(days []int, obs []evidence.Observation, seals []string, points []string) bool {
	type cell struct{}
	covered := make(map[string]cell, len(obs))
	for _, o := range obs {
		covered[fmt.Sprintf("%d\x00%s\x00%s", o.AgeDay, o.BasketSeal, o.PointID)] = cell{}
	}
	for _, day := range days {
		for _, s := range seals {
			for _, p := range points {
				if _, ok := covered[fmt.Sprintf("%d\x00%s\x00%s", day, s, p)]; !ok {
					return false
				}
			}
		}
	}
	return true
}

// computeWaterLossRate returns the fixed-point water-loss rate (loss/total).
func computeWaterLossRate(waterLoss, total int) (fixed.Value, error) {
	if total <= 0 {
		if waterLoss == 0 {
			return fixed.New(0), nil
		}
		return fixed.New(0), errors.New("water loss with zero total blocks")
	}
	return fixed.New(int64(waterLoss)).Div(fixed.New(int64(total)))
}

func credentialFor(id task.ID, v arbiter.Verdict, clock int64) string {
	sum := sha256.Sum256([]byte(string(id) + ":" + string(v) + ":" + strconv.FormatInt(clock, 10)))
	return hex.EncodeToString(sum[:])[:16]
}

// Finalize implements arbiter.Arbiter: the single-writer final barrier exposed
// over the service, returning the immutable terminal decision.
func (s *SQLite) Finalize(id task.ID, gen task.Generation) (arbiter.FinalDecision, error) {
	res, err := s.FinalizeTask(context.Background(), id, FinalizeRequest{Generation: gen})
	if err != nil {
		return arbiter.FinalDecision{}, err
	}
	return arbiter.FinalDecision{
		TaskID: id, Verdict: res.Verdict, Generation: gen,
		Credential: res.Credential, Reason: res.Reason,
	}, nil
}
