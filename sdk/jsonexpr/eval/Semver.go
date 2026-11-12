package eval

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// CompareSemver compares permissively parsed semantic-version-like values.
func CompareSemver(lhs, rhs interface{}) (int, bool) {
	l, lok := semverString(lhs)
	r, rok := semverString(rhs)
	if !lok || !rok {
		return 0, false
	}
	lv, lok := parseVersion(l)
	rv, rok := parseVersion(r)
	if !lok || !rok {
		return 0, false
	}
	for i := 0; i < max(len(lv.core), len(rv.core)); i++ {
		a, b := "0", "0"
		if i < len(lv.core) {
			a = lv.core[i]
		}
		if i < len(rv.core) {
			b = rv.core[i]
		}
		if c := compareIdentifier(a, b); c != 0 {
			return c, true
		}
	}
	if lv.pre == "" && rv.pre != "" {
		return 1, true
	}
	if lv.pre != "" && rv.pre == "" {
		return -1, true
	}
	if lv.pre == "" && rv.pre == "" {
		return 0, true
	}
	la, lb := strings.Split(lv.pre, "."), strings.Split(rv.pre, ".")
	for i := 0; i < max(len(la), len(lb)); i++ {
		if i >= len(la) {
			return -1, true
		}
		if i >= len(lb) {
			return 1, true
		}
		if c := compareIdentifier(la[i], lb[i]); c != 0 {
			return c, true
		}
	}
	return 0, true
}

type semver struct {
	core []string
	pre  string
}

func semverString(v interface{}) (string, bool) {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return "", false
	}
	switch rv.Kind() {
	case reflect.String:
		return rv.String(), true
	case reflect.Bool:
		return strconv.FormatBool(rv.Bool()), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(rv.Int(), 10), true
	case reflect.Float32, reflect.Float64:
		return fmt.Sprintf("%g", rv.Float()), true
	}
	return "", false
}
func parseVersion(s string) (semver, bool) {
	s = strings.TrimPrefix(strings.TrimPrefix(s, "v"), "V")
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return semver{}, false
	}
	p := ""
	if i := strings.IndexByte(s, '-'); i >= 0 {
		p = s[i+1:]
		s = s[:i]
	}
	if s == "" {
		return semver{}, false
	}
	return semver{strings.Split(s, "."), p}, true
}
func numeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
func compareIdentifier(a, b string) int {
	an, bn := numeric(a), numeric(b)
	if an && bn {
		a = strings.TrimLeft(a, "0")
		b = strings.TrimLeft(b, "0")
		if a == "" {
			a = "0"
		}
		if b == "" {
			b = "0"
		}
		if len(a) != len(b) {
			if len(a) > len(b) {
				return 1
			}
			return -1
		}
	} else if an {
		return -1
	} else if bn {
		return 1
	}
	return strings.Compare(a, b)
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
