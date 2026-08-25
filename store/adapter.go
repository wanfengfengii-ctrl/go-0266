package store

import (
	"context"
	"errors"
	"fmt"

	"potatoeye-cutseed-sprout-gate/task"
)

// Adapter failure kinds. A rejected, disconnected, timeout or malformed adapter
// reading produces only a retry-pending call record, never a valid evidence
// entry and never an early lease release (failure boundary 4 / acceptance 6).
var (
	ErrAdapterRejected     = errors.New("adapter: rejected")
	ErrAdapterDisconnected = errors.New("adapter: disconnected")
	ErrAdapterTimeout      = errors.New("adapter: timeout")
	ErrAdapterMalformed    = errors.New("adapter: malformed")
)

// Adapter names for the three external instruments.
const (
	adapterProbe = "probe"        // 温湿度探头
	adapterPlate = "plate_reader" // 快检读板器
	adapterScale = "scale"        // 称重适配器
)

func adapterStatus(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, ErrAdapterRejected):
		return "rejected"
	case errors.Is(err, ErrAdapterDisconnected):
		return "disconnected"
	case errors.Is(err, ErrAdapterTimeout):
		return "timeout"
	case errors.Is(err, ErrAdapterMalformed):
		return "malformed"
	default:
		return err.Error()
	}
}

// adapterFault returns the injected fault for an (adapter, target) key, or nil.
func (s *SQLite) adapterFault(adapter, target string) error {
	return s.faultFor(adapter + ":" + target)
}

// insertAdapterCall records one external instrument attempt. retryCount is the
// number of prior attempts for the same task/adapter/target.
func insertAdapterCall(ctx context.Context, q dbtx, id task.ID, adapter, target, status string, clock int64) (string, int, error) {
	var prior int
	if err := q.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM adapter_calls WHERE task_id = ? AND adapter = ? AND target = ?",
		id, adapter, target).Scan(&prior); err != nil {
		return "", 0, err
	}
	callID := fmt.Sprintf("%s-%s-%d", adapter, target, clock)
	if _, err := q.ExecContext(ctx,
		"INSERT INTO adapter_calls (call_id, task_id, adapter, target, status, retry_count, clock) VALUES (?,?,?,?,?,?,?)",
		callID, id, adapter, target, status, prior, clock); err != nil {
		return "", 0, err
	}
	return callID, prior, nil
}

func loadAdapterCalls(ctx context.Context, q dbtx, id task.ID) ([]AdapterCall, error) {
	rows, err := q.QueryContext(ctx,
		"SELECT call_id, task_id, adapter, target, status, retry_count, clock FROM adapter_calls WHERE task_id = ? ORDER BY clock", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AdapterCall
	for rows.Next() {
		var c AdapterCall
		if err := rows.Scan(&c.CallID, &c.TaskID, &c.Adapter, &c.Target, &c.Status, &c.RetryCount, &c.Clock); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// AdapterCall is one recorded external instrument attempt.
type AdapterCall struct {
	CallID     string
	TaskID     task.ID
	Adapter    string
	Target     string
	Status     string
	RetryCount int
	Clock      int64
}
