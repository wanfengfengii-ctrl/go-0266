// Package catalog implements the seed-variety and healing-rule directory.
//
// It owns the stable identifiers a cut-seed batch is locked against: breeding
// plots, variety generations, cellar summaries, cut-knife disinfection batches,
// threshold snapshots, qualified-personnel roles and rule versions (component
// traceability: 种薯品种与愈伤规则目录 -> catalog).
package catalog

import (
	"errors"

	"potatoeye-cutseed-sprout-gate/fixed"
)

// ErrMismatch reports a catalog-consistency rejection during lock validation.
var ErrMismatch = errors.New("catalog: mismatch")

// Stable identifiers frozen at lock time.
type (
	// Plot is a breeding plot (繁育地块).
	Plot string
	// VarietyGeneration is a variety generation (品种代次).
	VarietyGeneration string
	// CellarSummary is the seed-cellar summary (原薯窖位摘要).
	CellarSummary string
	// DisinfectBatch is the cut-knife disinfection batch (切刀消毒批次).
	DisinfectBatch string
	// PersonnelID identifies a qualified worker (人员 ID).
	PersonnelID string
	// Role is a qualified-personnel role.
	Role string
	// RuleVersion identifies a frozen rule version.
	RuleVersion int
)

// Qualified personnel roles. Independent review requires a reviewer who is
// distinct from any cutter on the same task (domain rule / acceptance 3).
const (
	RoleCutter   Role = "cutter"   // 切块人员
	RoleReviewer Role = "reviewer" // 复核人员
)

// Thresholds is a frozen threshold snapshot used by the final arbiter.
type Thresholds struct {
	// SuberizationGrade is the minimum suberization (corking) grade.
	SuberizationGrade int
	// WaterLossRate is the maximum allowed water-loss rate (fixed-point).
	WaterLossRate fixed.Value
	// DiseaseCt is the disease rapid-test Ct threshold (fixed-point).
	DiseaseCt fixed.Value
}

// Valid reports whether all threshold components are non-negative.
func (t Thresholds) Valid() bool {
	return t.SuberizationGrade >= 0 &&
		t.WaterLossRate.Raw() >= 0 &&
		t.DiseaseCt.Raw() >= 0
}

// Rule binds a plot/variety pair to its cellar, disinfection batch, threshold
// snapshot and the observation day-ages it must cover.
type Rule struct {
	Version     RuleVersion
	Plot        Plot
	Variety     VarietyGeneration
	Cellar      CellarSummary
	Disinfect   DisinfectBatch
	Thresholds  Thresholds
	ObserveDays []int
}

// Catalog is the read boundary the lock step validates against. Implementations
// answer consistency questions about plots, varieties, cellar summaries,
// disinfection batches, thresholds and personnel qualification.
type Catalog interface {
	// Match returns the rule for a plot/variety pair, or ErrMismatch.
	Match(plot Plot, variety VarietyGeneration) (Rule, error)
	// Qualified reports whether p holds role under the given rule version.
	Qualified(p PersonnelID, role Role, version RuleVersion) bool
}
