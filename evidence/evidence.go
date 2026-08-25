// Package evidence implements the healing day-age and bud-eye collection book,
// including the coverage matrix, fixed-point physics readings and the immutable
// disease rejudgement evidence version chain (component traceability: 愈伤日龄
// 及芽眼采集账簿 -> evidence coverage 模块; 病害复判及终局仲裁器 -> evidence).
package evidence

import (
	"potatoeye-cutseed-sprout-gate/fixed"
	"potatoeye-cutseed-sprout-gate/task"
)

type (
	// AgeDay is a healing observation day-age (愈伤日龄).
	AgeDay int
	// BasketSeal is a basket seal identifier (网筐封签).
	BasketSeal string
	// PointID is a bud-eye coverage point (芽眼点位).
	PointID string
	// WellID is a rapid-test plate well (快检板孔).
	WellID string
	// Version is an immutable evidence version (不可覆盖证据版本).
	Version int
)

// Conserved reports the integer conservation rule for a coverage cell: the
// healthy (suberized), water-loss and rot block counts must be non-negative and
// sum exactly to the total block count (domain rule 5 / acceptance 4).
func Conserved(total, suberized, waterLoss, rot int) bool {
	return total >= 0 && suberized >= 0 && waterLoss >= 0 && rot >= 0 &&
		suberized+waterLoss+rot == total
}

// BudEyeWithinBounds reports whether an active bud-eye count lies within the
// closed interval [0, capacity] (acceptance 4).
func BudEyeWithinBounds(active, capacity int) bool {
	return active >= 0 && capacity >= 0 && active <= capacity
}

// Observation is one cell of the healing coverage matrix.
type Observation struct {
	TaskID     task.ID
	AgeDay     AgeDay
	BasketSeal BasketSeal
	PointID    PointID
	// SuberizationGrade is the corking grade (木栓化等级).
	SuberizationGrade int
	// WaterLossBlocks, RotBlocks and SuberizedBlocks must conserve with total.
	WaterLossBlocks int
	RotBlocks       int
	SuberizedBlocks int
	TotalBlocks     int
	// BudEyeActive is the active bud-eye count at this point.
	BudEyeActive int
	// Remedial marks a supplementary measurement (补测标记).
	Remedial bool
	Version  Version
}

// DiseaseEvidence is one immutable entry in the rapid-test evidence chain.
type DiseaseEvidence struct {
	TaskID    task.ID
	BlindCode string
	WellID    WellID
	// LesionDiameter is the lesion diameter (病斑直径, fixed-point).
	LesionDiameter fixed.Value
	// ExudateLevel is the exudate grade (渗液等级).
	ExudateLevel int
	// CtValue is the rapid-test Ct value (Ct 定点值, fixed-point).
	CtValue fixed.Value
	// RejudgeGen is the rejudgement generation (复判代次).
	RejudgeGen task.Generation
	// SourceCallID links to the adapter call that produced the reading.
	SourceCallID string
	// Version is immutable once written (不可覆盖版本).
	Version Version
}

// Evidence is the boundary for appending coverage cells and disease evidence.
type Evidence interface {
	AppendObservation(o Observation) error
	AppendDisease(d DiseaseEvidence) error
}
