package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"

	"potatoeye-cutseed-sprout-gate/catalog"
	"potatoeye-cutseed-sprout-gate/ledger"
	"potatoeye-cutseed-sprout-gate/task"
)

// Lock freezes every identifier, resource window and threshold snapshot for a
// cut-seed batch and generates the task generation (business flow 1). It runs
// in a single transaction so a conflict rolls back every lease atomically.
//
// The frontend re-sends POST /v1/tasks/lock verbatim with the same operation id
// after a network timeout. The idempotency lookup at the top of the
// transaction replays the task id, generation and leases from the first write
// instead of re-acquiring leases (which the first write already holds) and
// returning a resource-already-held conflict (acceptance 3).
func (s *SQLite) Lock(ctx context.Context, req LockRequest) (LockResult, error) {
	if err := validateLockRequest(req); err != nil {
		return LockResult{}, err
	}

	var result LockResult
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		if rec, ok, err := getIdempotency(ctx, tx, req.OperationID); err != nil {
			return err
		} else if ok {
			if rec.RequestHash == hashRequest(req) {
				return s.replayLock(ctx, tx, rec.TaskID, &result)
			}
			return errConflict("operation id reused with different content")
		}

		rule, err := s.Catalog().Match(req.Plot, req.Variety)
		if err != nil {
			return errInvalid("plot/variety mismatch", err.Error())
		}
		// Stale cellar summary or disinfection batch must reject (acceptance 1).
		if rule.Cellar != req.Cellar {
			return errConflict("cellar summary mismatch", fmt.Sprintf("cellar %q != %q", req.Cellar, rule.Cellar))
		}
		if rule.Disinfect != req.Disinfect {
			return errConflict("disinfect batch mismatch", fmt.Sprintf("disinfect %q != %q", req.Disinfect, rule.Disinfect))
		}
		for _, p := range req.Reviewers {
			if !s.Catalog().Qualified(p, catalog.RoleReviewer, rule.Version) {
				return errInvalid("reviewer not qualified", string(p))
			}
		}

		clock, err := s.nextClock(ctx, tx)
		if err != nil {
			return err
		}
		id := task.ID(fmt.Sprintf("task-%d", clock))

		leases := ledger.IdentityLeases(string(req.Batch), req.BasketSeals, req.BlindCodes)
		leaseKeys, err := acquireLeases(ctx, tx, id, task.Generation(1), leases, clock)
		if err != nil {
			return err
		}

		row := TaskRow{
			ID:                id,
			Batch:             req.Batch,
			State:             task.StatePendingCutConfirm,
			Generation:        task.Generation(1),
			Plot:              req.Plot,
			Variety:           req.Variety,
			Cellar:            req.Cellar,
			Disinfect:         req.Disinfect,
			SuberizationGrade: rule.Thresholds.SuberizationGrade,
			WaterLossRate:     rule.Thresholds.WaterLossRate.Raw(),
			DiseaseCt:         rule.Thresholds.DiseaseCt.Raw(),
			Reviewers:         req.Reviewers,
			ObserveDays:       rule.ObserveDays,
			Points:            req.Points,
			HealingShed:       req.HealingShed,
			ProbeWindow:       req.ProbeWindow,
			SproutSlot:        req.SproutSlot,
			TestWells:         req.TestWells,
			CreatedClock:      clock,
		}
		if err := insertTask(ctx, tx, row); err != nil {
			return err
		}
		if err := insertAudit(ctx, tx, id, clock, "task locked"); err != nil {
			return err
		}
		if err := recordIdempotency(ctx, tx, req.OperationID, id, task.Generation(1), req, LockResult{
			TaskID: id, Generation: task.Generation(1), Leases: leaseKeys,
		}, clock); err != nil {
			return err
		}

		result = LockResult{TaskID: id, Generation: task.Generation(1), Leases: leaseKeys}
		return nil
	})
	if err != nil {
		return LockResult{}, err
	}
	return result, nil
}

func validateLockRequest(req LockRequest) *Error {
	if req.Plot == "" || req.Variety == "" || req.Batch == "" {
		return errInvalid("plot, variety and batch are required")
	}
	if req.HealingShed == "" || req.ProbeWindow == "" || req.SproutSlot == "" {
		return errInvalid("healing shed, probe window and sprout slot are required")
	}
	if len(req.BasketSeals) == 0 || len(req.BasketSeals) != len(req.BlindCodes) {
		return errInvalid("basket seals and blind codes must be non-empty and equal in count")
	}
	if len(req.Points) == 0 || len(req.Reviewers) == 0 {
		return errInvalid("points and reviewers are required")
	}
	return nil
}

// acquireLeases atomically claims a set of leases inside the caller's
// transaction. The first unique-violation aborts the whole write.
func acquireLeases(ctx context.Context, tx *sql.Tx, id task.ID, gen task.Generation, leases []ledger.Lease, clock int64) ([]string, error) {
	// Deterministic ordering so conflict reasons are stable.
	sort.Slice(leases, func(i, j int) bool {
		return ledger.LeaseKey(leases[i].ResourceType, leases[i].ResourceKey) <
			ledger.LeaseKey(leases[j].ResourceType, leases[j].ResourceKey)
	})
	keys := make([]string, 0, len(leases))
	for _, l := range leases {
		l.TaskID = id
		l.Generation = gen
		l.State = ledger.LeaseOpen
		l.StartClock = clock
		if err := acquireLeaseTx(ctx, tx, l); err != nil {
			if errors.Is(err, ledger.ErrConflict) {
				return nil, errConflict("resource already held", err.Error())
			}
			return nil, err
		}
		keys = append(keys, ledger.LeaseKey(l.ResourceType, l.ResourceKey))
	}
	return keys, nil
}

// recordIdempotency stores an operation result for later idempotent replay. An
// empty operation ID skips recording.
func recordIdempotency(ctx context.Context, tx *sql.Tx, op task.OperationID, id task.ID, gen task.Generation, req, resp any, clock int64) error {
	if op == "" {
		return nil
	}
	return putIdempotency(ctx, tx, IdempotencyRecord{
		OperationID:  op,
		TaskID:       id,
		RequestHash:  hashRequest(req),
		ResponseHash: hashRequest(resp),
		Generation:   gen,
		CreatedClock: clock,
	})
}

// replayLock rebuilds the original LockResult from the persisted task and its
// resource leases, so a verbatim retry with the same operation id returns the
// first task id instead of failing on the leases the first write holds.
func (s *SQLite) replayLock(ctx context.Context, tx *sql.Tx, id task.ID, result *LockResult) error {
	row, err := getTaskRow(ctx, tx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return errNotFound("task not found")
		}
		return err
	}
	leaseKeys, err := loadLeaseKeys(ctx, tx, id)
	if err != nil {
		return err
	}
	*result = LockResult{TaskID: row.ID, Generation: row.Generation, Leases: leaseKeys}
	return nil
}
