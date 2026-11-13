package eval

import (
	"reflect"
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
	var lhsStr, lerr = e.StringConvert(lhs)
	var rhsStr, rerr = e.StringConvert(rhs)
	if lerr != nil || rerr != nil || lhsStr == "" || rhsStr == "" {
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
