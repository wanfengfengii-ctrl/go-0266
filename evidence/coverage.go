package evidence

import (
	"errors"
	"fmt"
)

// Coverage errors raised while validating the healing observation matrix and
// the bud-eye point counts (acceptance 4).
var (
	// ErrNotConserved reports a cell whose counts do not satisfy the integer
	// conservation rule.
	ErrNotConserved = errors.New("evidence: counts not conserved")
	// ErrBudEyeOutOfBounds reports an active bud-eye count outside [0,capacity].
	ErrBudEyeOutOfBounds = errors.New("evidence: bud-eye out of bounds")
	// ErrDuplicateCoverage reports a repeated (age, seal, point) cell.
	ErrDuplicateCoverage = errors.New("evidence: duplicate coverage")
	// ErrMissingCoverage reports an incomplete matrix for the locked day-age.
	ErrMissingCoverage = errors.New("evidence: missing coverage")
)

// ValidateCell enforces the integer conservation and bud-eye bound rules for a
// single coverage cell. It returns a descriptive error naming the offending
// basket seal and point so the store can produce deterministic reasons.
func ValidateCell(o Observation) error {
	if !Conserved(o.TotalBlocks, o.SuberizedBlocks, o.WaterLossBlocks, o.RotBlocks) {
		return fmt.Errorf("%w: seal %q point %q total=%d suberized=%d waterloss=%d rot=%d",
			ErrNotConserved, o.BasketSeal, o.PointID,
			o.TotalBlocks, o.SuberizedBlocks, o.WaterLossBlocks, o.RotBlocks)
	}
	if o.SuberizationGrade < 0 {
		return fmt.Errorf("evidence: negative suberization grade %d", o.SuberizationGrade)
	}
	return nil
}

// ValidateBudEyeCount enforces the bud-eye active-count bound for one point.
func ValidateBudEyeCount(seal BasketSeal, point PointID, active, capacity int) error {
	if !BudEyeWithinBounds(active, capacity) {
		return fmt.Errorf("%w: seal %q point %q active=%d capacity=%d",
			ErrBudEyeOutOfBounds, seal, point, active, capacity)
	}
	return nil
}

// DuplicateCell reports whether a (age, seal, point) cell is already present in
// the existing observation set.
func DuplicateCell(existing map[string]bool, age AgeDay, seal BasketSeal, point PointID) bool {
	return existing[cellKey(age, seal, point)]
}

// CoverageComplete reports whether the supplied cells cover every locked
// basket seal and point for a single day-age. Missing seals or points are named
// in the returned error so the store can emit deterministic reasons.
func CoverageComplete(age AgeDay, cells []Observation, seals []BasketSeal, points []PointID) error {
	seen := make(map[string]bool, len(cells))
	for _, c := range cells {
		if c.AgeDay != age {
			continue
		}
		seen[cellKey(c.AgeDay, c.BasketSeal, c.PointID)] = true
	}
	for _, s := range seals {
		for _, p := range points {
			if !seen[cellKey(age, s, p)] {
				return fmt.Errorf("%w: age %d seal %q point %q", ErrMissingCoverage, age, s, p)
			}
		}
	}
	return nil
}

func cellKey(age AgeDay, seal BasketSeal, point PointID) string {
	return fmt.Sprintf("%d\x00%s\x00%s", age, seal, point)
}
