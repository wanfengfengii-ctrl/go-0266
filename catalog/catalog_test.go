package catalog

import "testing"

func TestThresholdsValid(t *testing.T) {
	valid := Thresholds{SuberizationGrade: 3, WaterLossRate: 50, DiseaseCt: 38}
	if !valid.Valid() {
		t.Fatalf("Valid() = false; want true for %+v", valid)
	}
	bad := Thresholds{SuberizationGrade: -1, WaterLossRate: 50, DiseaseCt: 38}
	if bad.Valid() {
		t.Fatalf("Valid() = true; want false for negative grade")
	}
}

func TestRolesDistinct(t *testing.T) {
	if RoleCutter == RoleReviewer {
		t.Fatalf("cutter and reviewer roles must be distinct")
	}
}
