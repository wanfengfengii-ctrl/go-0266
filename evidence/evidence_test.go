package evidence

import "testing"

func TestConserved(t *testing.T) {
	if !Conserved(10, 7, 2, 1) {
		t.Errorf("10 = 7+2+1 must be conserved")
	}
	if Conserved(10, 8, 2, 1) {
		t.Errorf("7+2+1 != 10 must not be conserved")
	}
	if Conserved(10, -1, 2, 1) {
		t.Errorf("negative counts must not be conserved")
	}
}

func TestBudEyeWithinBounds(t *testing.T) {
	if !BudEyeWithinBounds(3, 10) {
		t.Errorf("3 within [0,10] must be accepted")
	}
	if BudEyeWithinBounds(11, 10) {
		t.Errorf("11 within [0,10] must be rejected")
	}
	if BudEyeWithinBounds(-1, 10) {
		t.Errorf("-1 within [0,10] must be rejected")
	}
}
