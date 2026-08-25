// Package task implements the cut-seed sprout-room admission aggregate.
//
// It owns the deterministic state machine for a single cut-seed batch, the
// generation (代次) guard carried by every advancement, idempotent operation
// keys and the frozen lock snapshot summary (component traceability: 切块入催芽
// 任务聚合 -> task).
package task

import (
	"errors"

	"potatoeye-cutseed-sprout-gate/catalog"
)

// ErrNotFound reports an unknown task ID.
var ErrNotFound = errors.New("task: not found")

type (
	// ID is a task identifier.
	ID string
	// BatchNumber is the cut-seed batch number (切块批号).
	BatchNumber string
	// Generation is the task generation (任务代次). Every advancement must carry
	// the current generation; stale generations are rejected (domain rule 1).
	Generation int
	// OperationID is an idempotency operation key (幂等操作号).
	OperationID string
)

// State is the deterministic state machine for a cut-seed batch.
type State int

const (
	StatePendingLock       State = iota // 待锁定
	StatePendingCutConfirm              // 待切块确认
	StateSealingSamples                 // 样本封存中
	StateOccupyingShed                  // 棚位占用中
	StateHealingObserving               // 愈伤观察中
	StateBudEyeVerifying                // 芽眼核验中
	StateDiseaseRetesting               // 病害复测中
	StatePendingReview                  // 待独立复核
	StateReadyToSprout                  // 可入催芽
	StateSprouted                       // 已入催芽
	StateDiseaseQuarantine              // 病害隔离
	StateCancelled                      // 已取消
)

var stateNames = [...]string{
	StatePendingLock:       "pending_lock",
	StatePendingCutConfirm: "pending_cut_confirm",
	StateSealingSamples:    "sealing_samples",
	StateOccupyingShed:     "occupying_shed",
	StateHealingObserving:  "healing_observing",
	StateBudEyeVerifying:   "bud_eye_verifying",
	StateDiseaseRetesting:  "disease_retesting",
	StatePendingReview:     "pending_review",
	StateReadyToSprout:     "ready_to_sprout",
	StateSprouted:          "sprouted",
	StateDiseaseQuarantine: "disease_quarantine",
	StateCancelled:         "cancelled",
}

// String returns the stable name of the state.
func (s State) String() string {
	if s < 0 || int(s) >= len(stateNames) {
		return "unknown"
	}
	return stateNames[s]
}

// Terminal reports whether the state is a final state. After a final state no
// late reading, rejudgement, review or ordinary advancement may change it
// (domain rule 8).
func (s State) Terminal() bool {
	switch s {
	case StateSprouted, StateDiseaseQuarantine, StateCancelled:
		return true
	default:
		return false
	}
}

// linear is the ordered happy-path progression of open states.
var linear = []State{
	StatePendingLock,
	StatePendingCutConfirm,
	StateSealingSamples,
	StateOccupyingShed,
	StateHealingObserving,
	StateBudEyeVerifying,
	StateDiseaseRetesting,
	StatePendingReview,
	StateReadyToSprout,
	StateSprouted,
}

// CanTransition reports whether next is a legal successor of s. Terminal
// outcomes (disease quarantine, cancellation) are reachable from any open
// state; the happy path advances one step at a time.
func (s State) CanTransition(next State) bool {
	if s.Terminal() {
		return false
	}
	if next == StateDiseaseQuarantine || next == StateCancelled {
		return true
	}
	for i, cur := range linear {
		if cur == s && i+1 < len(linear) && linear[i+1] == next {
			return true
		}
	}
	return false
}

// LockSnapshot is the frozen summary captured at lock time (acceptance 1).
type LockSnapshot struct {
	Plot           catalog.Plot
	Variety        catalog.VarietyGeneration
	CellarSummary  catalog.CellarSummary
	DisinfectBatch catalog.DisinfectBatch
	Thresholds     catalog.Thresholds
	Reviewers      []catalog.PersonnelID
}

// Task is the aggregate projection for a cut-seed batch.
type Task struct {
	ID         ID
	Batch      BatchNumber
	State      State
	Generation Generation
	Snapshot   LockSnapshot
	// FinalReason carries the deterministic terminal summary once finalized.
	FinalReason string
}

// Aggregate is the read boundary for task projections.
type Aggregate interface {
	// Current returns the aggregate for a task ID, or ErrNotFound.
	Current(id ID) (Task, error)
}
