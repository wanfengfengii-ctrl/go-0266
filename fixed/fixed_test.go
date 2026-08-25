package fixed

import (
	"math"
	"testing"
)

func TestAddSub(t *testing.T) {
	v, err := New(1500).Add(New(2500))
	if err != nil || v.Raw() != 4000 {
		t.Fatalf("Add = %d, %v; want 4000, nil", v.Raw(), err)
	}
	v, err = New(2500).Sub(New(1500))
	if err != nil || v.Raw() != 1000 {
		t.Fatalf("Sub = %d, %v; want 1000, nil", v.Raw(), err)
	}
	if _, err := New(math.MaxInt64).Add(New(1)); err != ErrOverflow {
		t.Fatalf("Add overflow err = %v; want ErrOverflow", err)
	}
}

func TestMulDiv(t *testing.T) {
	v, err := New(1500).Mul(New(2000))
	if err != nil || v.Raw() != 3000 {
		t.Fatalf("Mul = %d, %v; want 3000, nil", v.Raw(), err)
	}
	v, err = New(3000).Div(New(2000))
	if err != nil || v.Raw() != 1500 {
		t.Fatalf("Div = %d, %v; want 1500, nil", v.Raw(), err)
	}
	if _, err := New(1).Div(New(0)); err != ErrDivideZero {
		t.Fatalf("Div by zero err = %v; want ErrDivideZero", err)
	}
	if _, err := New(math.MaxInt64).Mul(New(2)); err != ErrOverflow {
		t.Fatalf("Mul overflow err = %v; want ErrOverflow", err)
	}
}

func TestString(t *testing.T) {
	cases := map[int64]string{
		1234:   "1.234",
		-1234:  "-1.234",
		0:      "0.000",
		1000:   "1.000",
		-1000:  "-1.000",
		500000: "500.000",
	}
	for raw, want := range cases {
		if got := New(raw).String(); got != want {
			t.Errorf("String(%d) = %q; want %q", raw, got, want)
		}
	}
}
