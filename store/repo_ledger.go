package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"potatoeye-cutseed-sprout-gate/ledger"
	"potatoeye-cutseed-sprout-gate/task"
)

// isUniqueViolation reports whether a SQLite error is a UNIQUE constraint
// failure, which is how concurrent double-booking is arbitrated (failure
// boundary 2).
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

func acquireLeaseTx(ctx context.Context, tx *sql.Tx, l ledger.Lease) error {
	state := int(l.State)
	_, err := tx.ExecContext(ctx,
		"INSERT INTO resource_leases (resource_type, resource_key, task_id, generation, state, start_clock, end_clock) VALUES (?,?,?,?,?,?,?)",
		l.ResourceType, l.ResourceKey, l.TaskID, l.Generation, state, l.StartClock, l.EndClock)
	if isUniqueViolation(err) {
		return fmt.Errorf("%w: %s:%s held by another open task", ledger.ErrConflict, l.ResourceType, l.ResourceKey)
	}
	return err
}

// Acquire implements ledger.Ledger for a standalone single-lease claim.
func (s *SQLite) Acquire(l ledger.Lease) error {
	return s.withTx(context.Background(), func(tx *sql.Tx) error {
		clock, err := s.nextClock(context.Background(), tx)
		if err != nil {
			return err
		}
		l.StartClock = clock
		return acquireLeaseTx(context.Background(), tx, l)
	})
}

// Held implements ledger.Ledger, reporting the open-task owner of a resource.
func (s *SQLite) Held(rt ledger.ResourceType, key ledger.ResourceKey) (task.ID, bool) {
	var tid task.ID
	err := s.db.QueryRow(
		"SELECT task_id FROM resource_leases WHERE resource_type = ? AND resource_key = ? AND state = 0",
		rt, key).Scan(&tid)
	if err != nil {
		return "", false
	}
	return tid, true
}

// insertBasketSample writes one sealed basket sample binding within a tx.
func insertBasketSample(ctx context.Context, tx *sql.Tx, sample ledger.BasketSample) error {
	sealed, revealed := 0, 0
	if sample.Sealed {
		sealed = 1
	}
	if sample.Revealed {
		revealed = 1
	}
	_, err := tx.ExecContext(ctx,
		"INSERT INTO basket_samples (task_id, basket_seal, blind_code, sealed, revealed, generation) VALUES (?,?,?,?,?,?)",
		sample.TaskID, sample.BasketSeal, sample.BlindCode, sealed, revealed, sample.Generation)
	if isUniqueViolation(err) {
		return fmt.Errorf("%w: seal %q or blind code %q", ledger.ErrConflict, sample.BasketSeal, sample.BlindCode)
	}
	return err
}

// loadBasketSamples returns the sealed samples of a task ordered by seal.
func loadBasketSamples(ctx context.Context, q dbtx, id task.ID) ([]ledger.BasketSample, error) {
	rows, err := q.QueryContext(ctx,
		"SELECT task_id, basket_seal, blind_code, sealed, revealed, generation FROM basket_samples WHERE task_id = ? ORDER BY basket_seal", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ledger.BasketSample
	for rows.Next() {
		var s ledger.BasketSample
		var sealed, revealed int
		if err := rows.Scan(&s.TaskID, &s.BasketSeal, &s.BlindCode, &sealed, &revealed, &s.Generation); err != nil {
			return nil, err
		}
		s.Sealed = sealed == 1
		s.Revealed = revealed == 1
		out = append(out, s)
	}
	return out, rows.Err()
}

// findBlindCode returns the sealed sample for a blind code, or an error when the
// code is unknown to the task (an early reveal attempt, domain rule 4).
func findBlindCode(ctx context.Context, q dbtx, id task.ID, code string) (ledger.BasketSample, error) {
	var s ledger.BasketSample
	var sealed, revealed int
	err := q.QueryRowContext(ctx,
		"SELECT task_id, basket_seal, blind_code, sealed, revealed, generation FROM basket_samples WHERE task_id = ? AND blind_code = ?",
		id, code).Scan(&s.TaskID, &s.BasketSeal, &s.BlindCode, &sealed, &revealed, &s.Generation)
	if err == sql.ErrNoRows {
		return s, fmt.Errorf("%w: unknown blind code %q", ledger.ErrBlindMismatch, code)
	}
	s.Sealed = sealed == 1
	s.Revealed = revealed == 1
	return s, err
}

// releaseLeases marks every still-open lease held by a task as released and
// stamps the end clock. The open-occupancy unique index then no longer covers
// those rows, so a new task may re-acquire the same resources (domain rule 2:
// uniqueness protects only open occupancy). The released rows are kept on disk
// as an auditable historical record. It is a no-op for tasks with no leases.
func releaseLeases(ctx context.Context, tx *sql.Tx, id task.ID, clock int64) error {
	_, err := tx.ExecContext(ctx,
		"UPDATE resource_leases SET state = 1, end_clock = ? WHERE task_id = ? AND state = 0",
		clock, id)
	return err
}

// loadResourceKeys returns the resource keys of a given type held by a task,
// ordered deterministically.
func loadResourceKeys(ctx context.Context, q dbtx, id task.ID, rt ledger.ResourceType) ([]string, error) {
	rows, err := q.QueryContext(ctx,
		"SELECT resource_key FROM resource_leases WHERE task_id = ? AND resource_type = ? ORDER BY resource_key", id, rt)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}
