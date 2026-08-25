package arbiter

import "testing"

func TestVerdictValid(t *testing.T) {
	for _, v := range []Verdict{
		VerdictReadyToSprout, VerdictDiseaseQuarantine, VerdictCancelled,
	} {
		if !v.Valid() {
			t.Errorf("%s must be a valid verdict", v)
		}
	}
	if Verdict("pending").Valid() {
		t.Errorf("non-terminal verdict must be invalid")
	}
}
