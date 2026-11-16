package eval

import (
	"math"
	"math/big"
	"reflect"
	"strconv"
	"strings"
)

// VersionCompare compares two semantic versions for the semver_* operators,
// returning -1, 0 or 1, or nil when either side is not a string, number or
// boolean, is empty, or has an empty version core. It mirrors the JS and Java
// SDKs: a leading "v"/"V" and build metadata are ignored, missing core parts
// count as 0, numeric identifiers compare numerically (leading zeros ignored)
// and rank below alphanumeric ones, and a release ranks above its
// pre-releases. Parsing is as permissive as the other SDKs', so a
// non-numeric core part ("1.x") is compared as an alphanumeric identifier.
func (e Evaluator) VersionCompare(lhs reflect.Value, rhs reflect.Value) interface{} {
	var lhsStr, lconv = semverString(lhs)
	var rhsStr, rconv = semverString(rhs)
	if !lconv || !rconv || lhsStr == "" || rhsStr == "" {
		return nil
	}

	var l, lok = parseSemver(lhsStr)
	var r, rok = parseSemver(rhsStr)
	if !lok || !rok {
		return nil
	}

	for i := 0; i < len(l.parts) || i < len(r.parts); i++ {
		var lPart, rPart = "0", "0"
		if i < len(l.parts) {
			lPart = l.parts[i]
		}
		if i < len(r.parts) {
			rPart = r.parts[i]
		}
		if result := compareIdentifiers(lPart, rPart); result != 0 {
			return result
		}
	}

	if l.preRelease == "" && r.preRelease == "" {
		return 0
	} else if l.preRelease == "" {
		return 1
	} else if r.preRelease == "" {
		return -1
	}

	var lPre = strings.Split(l.preRelease, ".")
	var rPre = strings.Split(r.preRelease, ".")
	for i := 0; i < len(lPre) || i < len(rPre); i++ {
		if i >= len(lPre) {
			return -1
		}
		if i >= len(rPre) {
			return 1
		}
		if result := compareIdentifiers(lPre[i], rPre[i]); result != 0 {
			return result
		}
	}
	return 0
}

type semver struct {
	parts      []string
	preRelease string
}

func parseSemver(version string) (semver, bool) {
	var v = version
	if strings.HasPrefix(v, "v") || strings.HasPrefix(v, "V") {
		v = v[1:]
	}
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	if v == "" {
		return semver{}, false
	}

	var core, preRelease = v, ""
	if i := strings.IndexByte(v, '-'); i >= 0 {
		core, preRelease = v[:i], v[i+1:]
	}
	if core == "" {
		return semver{}, false
	}
	return semver{parts: strings.Split(core, "."), preRelease: preRelease}, true
}

func isNumericIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func stripLeadingZeros(s string) string {
	var stripped = strings.TrimLeft(s, "0")
	if stripped == "" {
		return "0"
	}
	return stripped
}

func compareIdentifiers(a string, b string) int {
	var aIsNum = isNumericIdentifier(a)
	var bIsNum = isNumericIdentifier(b)

	if aIsNum && bIsNum {
		var aNorm = stripLeadingZeros(a)
		var bNorm = stripLeadingZeros(b)
		if len(aNorm) != len(bNorm) {
			if len(aNorm) > len(bNorm) {
				return 1
			}
			return -1
		}
		return strings.Compare(aNorm, bNorm)
	}
	if aIsNum {
		return -1
	}
	if bIsNum {
		return 1
	}
	return strings.Compare(a, b)
}

// semverString converts a semver operand to a string the way the JS SDK's
// stringConvert does: strings as is, booleans as "true"/"false", and numbers
// with jsNumberString. It is local to the semver operators; the general
// StringConvert used by the older operators is unchanged.
func semverString(x reflect.Value) (string, bool) {
	switch x.Kind() {
	case reflect.String:
		return x.String(), true
	case reflect.Bool:
		return strconv.FormatBool(x.Bool()), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(x.Int(), 10), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(x.Uint(), 10), true
	case reflect.Float32, reflect.Float64:
		return jsNumberString(x.Float()), true
	}
	return "", false
}

// jsNumberString formats a number like JavaScript's
// x.toFixed(15).replace(/\.?0{0,15}$/, ""): fifteen decimals, rounded from
// the exact binary value with ties away from zero, then trailing zeros and a
// trailing point dropped. Like toFixed, it uses exponent notation (JavaScript's
// shortest form, such as "1e+21") from 1e21 up, and keeps the sign of a
// negative number that rounds to zero ("-0"). The trailing-zero strip applies
// to the exponent form too, as the JS regex does ("1e+300" becomes "1e+3").
func jsNumberString(x float64) string {
	switch {
	case math.IsNaN(x):
		return "NaN"
	case math.IsInf(x, 1):
		return "Infinity"
	case math.IsInf(x, -1):
		return "-Infinity"
	case math.Abs(x) >= 1e21:
		return stripTrailingZeros(strconv.FormatFloat(x, 'g', -1, 64))
	}

	var negative = x < 0
	// The exact decimal expansion of a float64 has at most 1074 fractional
	// digits, so this prints it without rounding.
	var exact = new(big.Float).SetFloat64(math.Abs(x)).Text('f', 1074)
	var point = strings.IndexByte(exact, '.')
	var digits = []byte(exact[:point] + exact[point+1:point+16])
	if exact[point+16] >= '5' {
		var i = len(digits) - 1
		for ; i >= 0 && digits[i] == '9'; i-- {
			digits[i] = '0'
		}
		if i < 0 {
			digits = append([]byte{'1'}, digits...)
		} else {
			digits[i]++
		}
	}

	var intLen = len(digits) - 15
	var text = stripTrailingZeros(string(digits[:intLen]) + "." + string(digits[intLen:]))
	if negative {
		return "-" + text
	}
	return text
}

// stripTrailingZeros is JavaScript's s.replace(/\.?0{0,15}$/, ""): up to
// fifteen trailing zeros go, and the point before them when they are all that
// follows it.
func stripTrailingZeros(s string) string {
	var end = len(s)
	for end > 0 && len(s)-end < 15 && s[end-1] == '0' {
		end--
	}
	if end > 0 && s[end-1] == '.' {
		end--
	}
	return s[:end]
}
