package catalog

import "potatoeye-cutseed-sprout-gate/fixed"

// DefaultRegistry seeds a small deterministic rule and personnel directory used
// by the demo entry point and the integration tests. The values are fixed so
// that lock, observation and finalization behave identically after restart.
func DefaultRegistry() *Registry {
	reg := NewRegistry()
	reg.AddRule(Rule{
		Version:   1,
		Plot:      "P-01",
		Variety:   "V-G2",
		Cellar:    "C-01",
		Disinfect: "D-01",
		Thresholds: Thresholds{
			SuberizationGrade: 2,
			WaterLossRate:     fixed.New(5000),  // 5.000%
			DiseaseCt:         fixed.New(35000), // 35.000 Ct
		},
		ObserveDays: []int{1, 2, 3},
	})
	reg.AddRule(Rule{
		Version:   1,
		Plot:      "P-02",
		Variety:   "V-G3",
		Cellar:    "C-02",
		Disinfect: "D-02",
		Thresholds: Thresholds{
			SuberizationGrade: 3,
			WaterLossRate:     fixed.New(4000),  // 4.000%
			DiseaseCt:         fixed.New(34000), // 34.000 Ct
		},
		ObserveDays: []int{1, 2},
	})
	for _, p := range []PersonnelID{"cutter-1", "cutter-2"} {
		reg.AddPersonnel(p, RoleCutter, 1)
	}
	for _, p := range []PersonnelID{"reviewer-1", "reviewer-2"} {
		reg.AddPersonnel(p, RoleReviewer, 1)
	}
	return reg
}
