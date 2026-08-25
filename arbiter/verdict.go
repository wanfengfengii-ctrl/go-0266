package arbiter

import (
	"errors"

	"potatoeye-cutseed-sprout-gate/catalog"
	"potatoeye-cutseed-sprout-gate/fixed"
)

// ErrNotFinalizable reports an attempt to finalize before the evidence is
// closed or before two independent reviews are present.
var ErrNotFinalizable = errors.New("arbiter: not finalizable")

// EvidenceSummary is the closure projection consumed by the final verdict
// selection. The store computes it from the committed coverage, bud-eye and
// disease evidence before invoking the single-writer barrier.
type EvidenceSummary struct {
	// SuberizationOK reports every covered cell met the locked grade.
	SuberizationOK bool
	// WaterLossOK reports every cell's water-loss rate met the locked threshold.
	WaterLossOK bool
	// BudEyeOK reports every bud-eye point count stayed within capacity.
	BudEyeOK bool
	// DiseaseClean reports no disease evidence exceeded the locked thresholds.
	DiseaseClean bool
	// Complete reports full coverage over the locked day-ages and baskets.
	Complete bool
}

// SelectVerdict applies the locked thresholds and closure state to choose one
// of the three terminal outcomes. A disease-positive batch is quarantined; a
// fully closed batch that meets every threshold and has two independent reviews
// is ready to sprout; otherwise the barrier refuses with ErrNotFinalizable.
func SelectVerdict(summary EvidenceSummary, reviews int) (Verdict, error) {
	if !summary.DiseaseClean {
		return VerdictDiseaseQuarantine, nil
	}
	if !summary.Complete || !summary.SuberizationOK || !summary.WaterLossOK || !summary.BudEyeOK {
		return "", ErrNotFinalizable
	}
	if reviews < 2 {
		return "", ErrNotFinalizable
	}
	return VerdictReadyToSprout, nil
}

// CellMeetsThresholds reports whether one coverage cell satisfies the locked
// suberization grade and water-loss thresholds. waterLossRate is the computed
// fixed-point rate (water-loss blocks over total blocks).
func CellMeetsThresholds(grade int, waterLossRate fixed.Value, t catalog.Thresholds) (suberOK, waterOK bool) {
	suberOK = grade >= t.SuberizationGrade
	waterOK = waterLossRate.Raw() <= t.WaterLossRate.Raw()
	return suberOK, waterOK
}

// DiseaseExceedsThreshold reports whether a disease reading exceeds the locked
// thresholds, marking the batch for quarantine.
func DiseaseExceedsThreshold(lesion fixed.Value, exudate int, ct fixed.Value, t catalog.Thresholds) bool {
	if exudate > 0 {
		return true
	}
	if lesion.Raw() > 0 && lesion.Raw() > t.DiseaseCt.Raw() {
		return true
	}
	// A low Ct value indicates a strong positive; compare against the threshold.
	return ct.Raw() > 0 && ct.Raw() < t.DiseaseCt.Raw()
}
