package task

import (
	"errors"
	"fmt"
)

// Flow-level sentinel errors used across the lock, cut-confirmation and
// advancement steps. The store maps these to stable API error codes.
var (
	// ErrStaleGeneration reports a generation that does not match the task.
	ErrStaleGeneration = errors.New("task: stale generation")
	// ErrTerminal reports an advancement on a task already in a terminal state.
	ErrTerminal = errors.New("task: terminal state")
	// ErrSnapshotMismatch reports a cut confirmation whose snapshot differs
	// from the frozen lock snapshot.
	ErrSnapshotMismatch = errors.New("task: snapshot mismatch")
	// ErrDuplicateCutter reports two confirmations from the same cutter.
	ErrDuplicateCutter = errors.New("task: duplicate cutter")
)

// SnapshotEqual reports whether two lock snapshots agree field-for-field. It is
// the comparison used to reject a cut confirmation that does not match the
// frozen snapshot (acceptance 3).
func SnapshotEqual(a, b LockSnapshot) bool {
	if a.Plot != b.Plot || a.Variety != b.Variety ||
		a.CellarSummary != b.CellarSummary || a.DisinfectBatch != b.DisinfectBatch {
		return false
	}
	if a.Thresholds != b.Thresholds {
		return false
	}
	if len(a.Reviewers) != len(b.Reviewers) {
		return false
	}
	for i := range a.Reviewers {
		if a.Reviewers[i] != b.Reviewers[i] {
			return false
		}
	}
	return true
}

// GuardGeneration enforces domain rule 1: every advancement must carry the
// current generation. A stale generation is rejected with ErrStaleGeneration.
func GuardGeneration(current, supplied Generation) error {
	if supplied != current {
		return fmt.Errorf("%w: have %d want %d", ErrStaleGeneration, supplied, current)
	}
	return nil
}

// GuardOpen enforces domain rule 8: no advancement may change a terminal task.
func GuardOpen(s State) error {
	if s.Terminal() {
		return fmt.Errorf("%w: state %s", ErrTerminal, s)
	}
	return nil
}

// ConfirmCutters validates the two-person cut confirmation rule: two distinct
// qualified cutters must each confirm, and the confirmation content must match
// the frozen snapshot (business flow 2).
type CutConfirmation struct {
	PersonnelID string
	Snapshot    LockSnapshot
}

// ValidateCutConfirmation checks a single confirmation against the frozen
// snapshot and an existing set of already-confirmed cutters.
func ValidateCutConfirmation(conf CutConfirmation, frozen LockSnapshot, confirmed []string) error {
	if !SnapshotEqual(conf.Snapshot, frozen) {
		return ErrSnapshotMismatch
	}
	for _, p := range confirmed {
		if p == conf.PersonnelID {
			return ErrDuplicateCutter
		}
	}
	return nil
}
