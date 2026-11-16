package eval

import (
	"math"
	"reflect"
	"testing"
)

// Expected strings are JavaScript's x.toFixed(15).replace(/\.?0{0,15}$/, ""),
// computed with Node.
func TestJSNumberString(t *testing.T) {
	var cases = []struct {
		x    float64
		want string
	}{
		{1e-07, "0.0000001"},
		{-1e-07, "-0.0000001"},
		{1.5, "1.5"},
		{0.1, "0.1"},
		{0.30000000000000004, "0.3"},
		{1.0000000000000002, "1"},
		{1e-16, "0"},
		{-1e-16, "-0"},
		{5e-16, "0.000000000000001"},
		{1.52587890625e-05, "0.000015258789063"},
		{123.456, "123.456000000000003"},
		{1000000000000000.2, "1000000000000000.25"},
		{1e+20, "100000000000000000000"},
		{9.999999999999999e+20, "999999999999999868928"},
		{1e+21, "1e+21"},
		{-1e+21, "-1e+21"},
		{1.5e+21, "1.5e+21"},
		{1e+300, "1e+3"},
		{2.5, "2.5"},
		{0.0, "0"},
		{0.0, "0"},
		{5e-16, "0.000000000000001"},
		{1.5e-15, "0.000000000000001"},
		{1.0000000000000004, "1"},
		{9007199254740992.0, "9007199254740992"},
		{4.35, "4.35"},
		{1.005, "1.005"},
		{math.Copysign(0, -1), "0"},
		{math.NaN(), "NaN"},
		{math.Inf(1), "Infinity"},
		{math.Inf(-1), "-Infinity"},
	}
	for _, tc := range cases {
		if got := jsNumberString(tc.x); got != tc.want {
			t.Errorf("jsNumberString(%v) = %q, want %q", tc.x, got, tc.want)
		}
	}
}

func TestSemverNumericOperandsMatchJS(t *testing.T) {
	var e = Evaluator{}
	for _, tc := range []struct {
		lhs  interface{}
		rhs  interface{}
		want interface{}
	}{
		{1e-7, "0.0000001", 0},
		{1.0000000000000002, "1", 0},
		{1e-16, "0", 0},
		{1e21, "1e+21", 0},
		{2.5, "2.5.0", 0},
		{1e-7, "0.0000002", -1},
		{int64(3), "3.0", 0},
		{uint8(3), "3", 0},
	} {
		if got := e.VersionCompare(reflect.ValueOf(tc.lhs), reflect.ValueOf(tc.rhs)); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("VersionCompare(%#v, %#v) = %#v, want %#v", tc.lhs, tc.rhs, got, tc.want)
		}
	}
}

// The semver converter is local: the general StringConvert used by eq, in and
// match keeps its released formatting.
func TestStringConvertUnchanged(t *testing.T) {
	var got, err = Evaluator{}.StringConvert(reflect.ValueOf(1e-7))
	if err != nil || got != "1e-07" {
		t.Errorf("StringConvert(1e-7) = %q, %v; want the released \"1e-07\"", got, err)
	}
}
