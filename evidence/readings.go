package evidence

import (
	"errors"
	"fmt"

	"potatoeye-cutseed-sprout-gate/fixed"
)

// ErrReadingOutOfRange reports a fixed-point physical or chemistry reading that
// falls outside its accepted field range (domain rule 6).
var ErrReadingOutOfRange = errors.New("evidence: reading out of range")

// Field ranges for the physical and chemistry readings. All values are checked
// for length (magnitude) and sign before being written as derived evidence.
const (
	maxShedTemp     = 100000 // 100.000 C
	minShedTemp     = -40000 // -40.000 C
	maxHumidity     = 100000 // 100.000 %
	maxWeightChange = 500000 // 500.000 g
	maxCt           = 50000  // 50.000 Ct
	maxLesion       = 200000 // 200.000 mm
)

// ValidateShedTemp rejects a shed temperature outside the closed field range.
func ValidateShedTemp(v fixed.Value) error {
	return inRange("shed_temp", v, minShedTemp, maxShedTemp)
}

// ValidateHumidity rejects a relative humidity outside [0, 100] percent.
func ValidateHumidity(v fixed.Value) error {
	return inRange("humidity", v, 0, maxHumidity)
}

// ValidateWeightChange rejects a block-weight change outside the field range.
func ValidateWeightChange(v fixed.Value) error {
	return inRange("weight_change", v, 0, maxWeightChange)
}

// ValidateCt rejects a rapid-test Ct value outside [0, 50] Ct.
func ValidateCt(v fixed.Value) error {
	return inRange("ct", v, 0, maxCt)
}

// ValidateLesionDiameter rejects a lesion diameter outside [0, 200] mm.
func ValidateLesionDiameter(v fixed.Value) error {
	return inRange("lesion", v, 0, maxLesion)
}

func inRange(field string, v fixed.Value, lo, hi int64) error {
	raw := v.Raw()
	if raw < lo || raw > hi {
		return fmt.Errorf("%w: %s=%s not in [%s,%s]",
			ErrReadingOutOfRange, field, v, fixed.New(lo), fixed.New(hi))
	}
	return nil
}
