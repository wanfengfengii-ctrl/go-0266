package store

import (
	"context"
	"errors"

	"potatoeye-cutseed-sprout-gate/task"
)

// GetTask returns the task aggregate projection plus its terminal-closure flag
// and the deterministically ordered audit reasons.
func (s *SQLite) GetTask(ctx context.Context, id task.ID) (TaskDetail, error) {
	row, err := getTaskRow(ctx, s.db, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return TaskDetail{}, errNotFound("task not found")
		}
		return TaskDetail{}, err
	}
	detail := TaskDetail{Task: row.toTask(), Closed: row.State.Terminal()}
	events, err := s.Audit(ctx, id)
	if err != nil {
		return TaskDetail{}, err
	}
	for _, e := range events {
		detail.Reasons = append(detail.Reasons, e.Reason)
	}
	return detail, nil
}

// Audit returns the task's audit events ordered by their monotonic sequence,
// giving the deterministic sort used by the API audit query.
func (s *SQLite) Audit(ctx context.Context, id task.ID) ([]AuditEvent, error) {
	return loadAudit(ctx, s.db, id)
}

// hasFinalDecision reports whether a terminal decision has already been issued
// for the task. Once any verdict is issued no ordinary advancement may change
// the task, even in the ready_to_sprout (可入催芽) decision state.
func hasFinalDecision(ctx context.Context, q dbtx, id task.ID) bool {
	var n int
	if err := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM final_decisions WHERE task_id = ?", id).Scan(&n); err != nil {
		return false
	}
	return n > 0
}
