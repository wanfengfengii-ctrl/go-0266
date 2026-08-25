// Package fixed provides fixed-point integer arithmetic for the physical and
// chemistry readings used throughout the PotatoEye cut-seed sprout gate.
//
// All water-loss rate, shed temperature, relative humidity, block-weight
// change, rapid-test Ct and lesion-diameter values are carried as fixed-point
// integers scaled by Scale (three decimal places). Every arithmetic operation
// checks sign, length, division by zero and overflow, so a failed arithmetic
// operation can never produce derived evidence (domain rule 6).
package fixed

import (
	"errors"
	"fmt"
	"math"
)

// Scale is the number of decimal places carried by a Value.
const Scale int64 = 1000

var (
	// ErrOverflow reports an integer overflow during a fixed-point operation.
	ErrOverflow = errors.New("fixed: overflow")
	// ErrDivideZero reports a division by a zero divisor.
	ErrDivideZero = errors.New("fixed: division by zero")
)

// Value is a fixed-point integer encoding a decimal scaled by Scale.
// A Value of 1234 represents the decimal 1.234.
type Value int64

// New returns a fixed-point Value from an already-scaled raw integer.
func New(raw int64) Value { return Value(raw) }

// Raw returns the underlying scaled integer.
func (v Value) Raw() int64 { return int64(v) }

// Add returns v+o, reporting ErrOverflow on overflow.
func (v Value) Add(o Value) (Value, error) {
	a, b := int64(v), int64(o)
	if (b > 0 && a > math.MaxInt64-b) || (b < 0 && a < math.MinInt64-b) {
		return 0, ErrOverflow
	}
	return Value(a + b), nil
}

// Sub returns v-o, reporting ErrOverflow on overflow.
func (v Value) Sub(o Value) (Value, error) {
	a, b := int64(v), int64(o)
	if (b < 0 && a > math.MaxInt64+b) || (b > 0 && a < math.MinInt64+b) {
		return 0, ErrOverflow
	}
	return Value(a - b), nil
}

// Mul returns the fixed-point product (v*o)/Scale, reporting ErrOverflow on
// overflow of the intermediate product.
func (v Value) Mul(o Value) (Value, error) {
	a, b := int64(v), int64(o)
	if a == 0 || b == 0 {
		return 0, nil
	}
	if mulOverflows(a, b) {
		return 0, ErrOverflow
	}
	return Value(a * b / Scale), nil
}

// Div returns the fixed-point quotient (v*Scale)/o, reporting ErrDivideZero for
// a zero divisor and ErrOverflow when the intermediate numerator overflows.
func (v Value) Div(o Value) (Value, error) {
	a, b := int64(v), int64(o)
	if b == 0 {
		return 0, ErrDivideZero
	}
	if mulOverflows(a, Scale) {
		return 0, ErrOverflow
	}
	return Value(a * Scale / b), nil
}

// String renders the fixed-point value with three decimal places.
func (v Value) String() string {
	r := int64(v)
	neg := r < 0
	abs := r
	if neg {
		abs = -abs
	}
	whole := abs / Scale
	frac := abs % Scale
	s := fmt.Sprintf("%d.%03d", whole, frac)
	if neg {
		return "-" + s
	}
	return s
}

// mulOverflows reports whether a*b overflows int64.
func mulOverflows(a, b int64) bool {
	if a == 0 || b == 0 {
		return false
	}
	if a == math.MinInt64 && b == -1 || b == math.MinInt64 && a == -1 {
		return true
	}
	r := a * b
	return r/b != a
}
