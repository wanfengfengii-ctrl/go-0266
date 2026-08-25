// Package arbiter implements the disease rejudgement and final arbitration.
//
// It owns the single-writer final barrier that produces exactly one of three
// terminal verdicts: allow entry to the sprout room, disease quarantine or task
// cancellation (component traceability: 病害复判及终局仲裁器 -> arbiter).
package arbiter

import "potatoeye-cutseed-sprout-gate/task"

// Verdict is the terminal arbitration outcome.
type Verdict string

const (
	// VerdictReadyToSprout allows entry to the sprout room (允许入催芽).
	VerdictReadyToSprout Verdict = "ready_to_sprout"
	// VerdictDiseaseQuarantine isolates the batch for disease (病害隔离).
	VerdictDiseaseQuarantine Verdict = "disease_quarantine"
	// VerdictCancelled cancels the task (任务取消).
	VerdictCancelled Verdict = "cancelled"
)

// Valid reports whether the verdict is one of the three terminal outcomes.
func (v Verdict) Valid() bool {
	switch v {
	case VerdictReadyToSprout, VerdictDiseaseQuarantine, VerdictCancelled:
		return true
	default:
		return false
	}
}

// Review is one independent reviewer's opinion. Two distinct qualified
// reviewers must agree before a ready-to-sprout verdict is issued.
type Review struct {
	TaskID      task.ID
	PersonnelID string
	Decision    Verdict
	Generation  task.Generation
}

// FinalDecision is the single immutable terminal result.
type FinalDecision struct {
	TaskID     task.ID
	Verdict    Verdict
	Generation task.Generation
	// Credential is the terminal credential (终局凭据).
	Credential string
	Reason     string
}

// Arbiter is the single-writer final boundary.
type Arbiter interface {
	Finalize(id task.ID, gen task.Generation) (FinalDecision, error)
}
