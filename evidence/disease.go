package evidence

import (
	"errors"
	"fmt"

	"potatoeye-cutseed-sprout-gate/task"
)

// ErrImmutableVersion reports an attempt to overwrite an existing evidence
// version (domain rule 7: 不可覆盖证据版本链).
var ErrImmutableVersion = errors.New("evidence: immutable version")

// ErrRejudgeExists reports a second rejudgement for a generation that already
// carries one (acceptance 7: 异常筐位只创建一个当前代次复判).
var ErrRejudgeExists = errors.New("evidence: rejudgement already exists")

// NextVersion returns the next immutable evidence version for a task. Versions
// increase monotonically so a late reading with an old version can never
// overwrite the current chain.
func NextVersion(current Version) Version { return current + 1 }

// ValidateDisease checks a disease-evidence entry's fixed-point fields and its
// version, returning a descriptive error naming the offending blind code and
// well so the store can emit deterministic reasons.
func ValidateDisease(d DiseaseEvidence) error {
	if err := ValidateLesionDiameter(d.LesionDiameter); err != nil {
		return fmt.Errorf("blind %q well %q: %w", d.BlindCode, d.WellID, err)
	}
	if err := ValidateCt(d.CtValue); err != nil {
		return fmt.Errorf("blind %q well %q: %w", d.BlindCode, d.WellID, err)
	}
	if d.ExudateLevel < 0 {
		return fmt.Errorf("blind %q well %q: negative exudate level %d", d.BlindCode, d.WellID, d.ExudateLevel)
	}
	return nil
}

// GuardImmutable rejects a write that would replace an already-committed
// evidence version for the same task/blind/well.
func GuardImmutable(existing map[string]Version, d DiseaseEvidence) error {
	key := d.BlindCode + "\x00" + string(d.WellID)
	if v, ok := existing[key]; ok && v >= d.Version {
		return fmt.Errorf("%w: blind %q well %q version %d <= %d",
			ErrImmutableVersion, d.BlindCode, d.WellID, d.Version, v)
	}
	return nil
}

// GuardRejudge rejects a second rejudgement for a generation that already has
// one, keeping the rejudgement evidence chain single per generation.
func GuardRejudge(existingGen map[task.Generation]bool, gen task.Generation) error {
	if existingGen[gen] {
		return fmt.Errorf("%w: generation %d", ErrRejudgeExists, gen)
	}
	return nil
}
