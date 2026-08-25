package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"

	"potatoeye-cutseed-sprout-gate/arbiter"
	"potatoeye-cutseed-sprout-gate/catalog"
	"potatoeye-cutseed-sprout-gate/task"
)

// hashRequest produces a deterministic content hash used to detect idempotency
// conflicts (same operation ID with different content).
func hashRequest(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// IdempotencyRecord is one operation-keyed result (acceptance 3).
type IdempotencyRecord struct {
	OperationID  task.OperationID
	TaskID       task.ID
	RequestHash  string
	ResponseHash string
	Generation   task.Generation
	CreatedClock int64
	Conflict     bool
}

func getIdempotency(ctx context.Context, q dbtx, op task.OperationID) (IdempotencyRecord, bool, error) {
	var r IdempotencyRecord
	var conflict int
	err := q.QueryRowContext(ctx,
		"SELECT operation_id, task_id, request_hash, response_hash, generation, created_clock, conflict FROM idempotency_records WHERE operation_id = ?",
		op).Scan(&r.OperationID, &r.TaskID, &r.RequestHash, &r.ResponseHash, &r.Generation, &r.CreatedClock, &conflict)
	if err == sql.ErrNoRows {
		return r, false, nil
	}
	if err != nil {
		return r, false, err
	}
	r.Conflict = conflict == 1
	return r, true, nil
}

func putIdempotency(ctx context.Context, tx *sql.Tx, r IdempotencyRecord) error {
	conflict := 0
	if r.Conflict {
		conflict = 1
	}
	_, err := tx.ExecContext(ctx,
		"INSERT INTO idempotency_records (operation_id, task_id, request_hash, response_hash, generation, created_clock, conflict) VALUES (?,?,?,?,?,?,?)",
		r.OperationID, r.TaskID, r.RequestHash, r.ResponseHash, r.Generation, r.CreatedClock, conflict)
	return err
}

func insertReview(ctx context.Context, tx *sql.Tx, review arbiter.Review) error {
	_, err := tx.ExecContext(ctx,
		"INSERT INTO reviews (task_id, personnel_id, decision, generation) VALUES (?,?,?,?)",
		review.TaskID, review.PersonnelID, review.Decision, review.Generation)
	return err
}

func loadReviews(ctx context.Context, q dbtx, id task.ID) ([]arbiter.Review, error) {
	rows, err := q.QueryContext(ctx,
		"SELECT task_id, personnel_id, decision, generation FROM reviews WHERE task_id = ? ORDER BY personnel_id", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []arbiter.Review
	for rows.Next() {
		var r arbiter.Review
		if err := rows.Scan(&r.TaskID, &r.PersonnelID, &r.Decision, &r.Generation); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func insertFinalDecision(ctx context.Context, tx *sql.Tx, d arbiter.FinalDecision) error {
	_, err := tx.ExecContext(ctx,
		"INSERT INTO final_decisions (task_id, verdict, generation, credential, reason) VALUES (?,?,?,?,?)",
		d.TaskID, d.Verdict, d.Generation, d.Credential, d.Reason)
	return err
}

func loadFinalDecision(ctx context.Context, q dbtx, id task.ID) (arbiter.FinalDecision, bool, error) {
	var d arbiter.FinalDecision
	err := q.QueryRowContext(ctx,
		"SELECT task_id, verdict, generation, credential, reason FROM final_decisions WHERE task_id = ?",
		id).Scan(&d.TaskID, &d.Verdict, &d.Generation, &d.Credential, &d.Reason)
	if err == sql.ErrNoRows {
		return d, false, nil
	}
	return d, err == nil, err
}

func insertAudit(ctx context.Context, tx *sql.Tx, id task.ID, clock int64, reason string) error {
	_, err := tx.ExecContext(ctx,
		"INSERT INTO audit_events (task_id, clock, reason) VALUES (?,?,?)", id, clock, reason)
	return err
}

func loadAudit(ctx context.Context, q dbtx, id task.ID) ([]AuditEvent, error) {
	rows, err := q.QueryContext(ctx,
		"SELECT seq, task_id, clock, reason FROM audit_events WHERE task_id = ? ORDER BY seq", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEvent
	for rows.Next() {
		var e AuditEvent
		if err := rows.Scan(&e.Seq, &e.TaskID, &e.Clock, &e.Reason); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// loadCutConfirmers returns the personnel who already confirmed the cut, used to
// enforce the two-distinct-cutter rule.
func loadCutConfirmers(ctx context.Context, q dbtx, id task.ID) ([]catalog.PersonnelID, error) {
	rows, err := q.QueryContext(ctx,
		"SELECT personnel_id FROM cut_confirmations WHERE task_id = ? ORDER BY personnel_id", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []catalog.PersonnelID
	for rows.Next() {
		var p catalog.PersonnelID
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func insertCutConfirmation(ctx context.Context, tx *sql.Tx, id task.ID, p catalog.PersonnelID) error {
	_, err := tx.ExecContext(ctx,
		"INSERT INTO cut_confirmations (task_id, personnel_id) VALUES (?,?)", id, p)
	return err
}
