package eval

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strconv"
	"strings"
)

type Evaluator struct {
	Operators map[string]Operator
	Vars      map[string]interface{}
}

func (e Evaluator) Evaluate(expr reflect.Value) interface{} {
	if expr.Kind() == reflect.Slice || expr.Kind() == reflect.Array {
		return e.Operators["and"].Evaluate(e, expr.Interface())
	} else if expr.Kind() == reflect.Map {
		var entry = expr.MapRange()
		if !entry.Next() {
			return nil
		}
		var op = e.Operators[entry.Key().String()]
		if op != nil {
			return op.Evaluate(e, entry.Value().Interface())
		}
	}
	return nil
}

func (e Evaluator) Compare(lhs reflect.Value, rhs reflect.Value) interface{} {
	if lhs.IsValid() && rhs.IsValid() {
		if lhs.IsZero() && rhs.IsZero() {
			return 0
		} else if lhs.Interface() == nil && rhs.Interface() == nil {
			return 0
		}
	}
	// After the zero shortcut, which compares the operands as given: a
	// json.Number zero is not the empty string.
	lhs = numberValue(lhs)

	if lhs.Kind() == reflect.Int || lhs.Kind() == reflect.Float64 {
		var rvalue, rerror = e.NumberConvert(rhs)
		var lvalue, lerror = e.NumberConvert(lhs)
		if rerror == nil && lerror == nil {
			if lvalue == rvalue {
				return 0
			} else if lvalue > rvalue {
				return 1
			} else {
				return -1
			}
		}
	} else if lhs.Kind() == reflect.String {
		var rvalue, rerror = e.StringConvert(rhs)
		if rerror == nil {
			return strings.Compare(lhs.String(), rvalue)
		}
	} else if lhs.Kind() == reflect.Bool {
		var rvalue = e.BooleanConvert(rhs)

		if lhs.Bool() == rvalue {
			return 0
		} else if lhs.Bool() {
			return 1
		} else {
			return -1
		}
	}

	if isCollection(lhs) && isCollection(rhs) {
		if deepEquals(lhs, rhs) {
			return 0
		}
		return nil
	}

	if lhs.IsValid() && rhs.IsValid() && lhs.Kind() == rhs.Kind() {
		if lhs.Comparable() && rhs.Comparable() && lhs.Interface() == rhs.Interface() {
			return 0
		}
		if reflect.DeepEqual(lhs.Interface(), rhs.Interface()) {
			return 0
		}
	}

	if !lhs.IsValid() && !rhs.IsValid() {
		return 0
	}

	return nil
}

func (e Evaluator) BooleanConvert(x reflect.Value) bool {
	x = numberValue(x)
	if x.Kind() == reflect.Bool {
		return x.Bool()
	} else if x.Kind() == reflect.String {
		var str = x.String()
		return str != "false" && str != "0" && str != ""
	} else if x.Kind() == reflect.Int {
		return x.Int() != 0
	} else if x.Kind() == reflect.Float64 {
		return x.Float() != 0
	} else if x.IsValid() && (x.Interface() == false || x.Interface() == "false" || x.Interface() == "[false]") {
		return false
	} else if x.IsValid() && (x.Interface() == true || x.Interface() == "true" || x.Interface() == "[true]") {
		return true
	} else if !x.IsValid() {
		return false
	}

	kind := x.Kind()
	if kind == reflect.Chan || kind == reflect.Func || kind == reflect.Interface ||
		kind == reflect.Map || kind == reflect.Pointer || kind == reflect.Slice {
		return !x.IsNil()
	}

	return true
}

func (e Evaluator) NumberConvert(x reflect.Value) (float64, error) {
	x = numberValue(x)
	if x.Kind() == reflect.Bool {
		if x.Bool() {
			return 1.0, nil
		} else {
			return 0.0, nil
		}
	} else if x.Kind() == reflect.String {
		if s, err := strconv.ParseFloat(x.String(), 64); err == nil {
			return s, nil
		}
	} else if x.Kind() == reflect.Int {
		return float64(x.Int()), nil
	} else if x.Kind() == reflect.Float64 {
		return x.Float(), nil
	}
	return 0.0, errors.New("can't convert number")
}

func (e Evaluator) StringConvert(x reflect.Value) (string, error) {
	if x.IsValid() && x.Type() == jsonNumberType {
		if number := numberValue(x); number.Kind() == reflect.Float64 {
			return jsNumberString(number.Float()), nil
		}
	}
	if x.Kind() == reflect.Bool {
		return strconv.FormatBool(x.Bool()), nil
	} else if x.Kind() == reflect.String {
		return x.String(), nil
	} else if x.Kind() == reflect.Int {
		return strconv.FormatInt(x.Int(), 10), nil
	} else if x.Kind() == reflect.Float64 {
		return strconv.FormatFloat(x.Float(), 'g', 16, 64), nil
	}
	return "", errors.New("can't convert string")
}

func (e Evaluator) ExtractVar(path string) interface{} {
	var frags = strings.Split(path, "/")

	var target interface{}
	if e.Vars != nil {
		target = e.Vars
	}

	for i := 0; i < len(frags); i++ {
		var tp = reflect.TypeOf(target)
		var vlof = reflect.ValueOf(target)
		var value interface{}
		if tp.Kind() == reflect.Slice || tp.Kind() == reflect.Array {
			if result, err := strconv.ParseInt(frags[i], 10, 32); err == nil {
				if vlof.Len() <= int(result) {
					return nil
				}
				var tmp = vlof.Index(int(result))
				if tmp.IsValid() && !tmp.IsZero() {
					value = tmp.Interface()
				}
			}
		} else if tp.Kind() == reflect.Map {
			var tmp = vlof.MapIndex(reflect.ValueOf(frags[i]))
			if tmp.IsValid() && !tmp.IsZero() {
				value = tmp.Interface()
			}
		}

		if value != nil {
			target = value
			continue
		}

		return nil
	}

	return target
}

var jsonNumberType = reflect.TypeOf(json.Number(""))

// numberValue is a json.Number (a value decoded with UseNumber) as the
// float64 it denotes, as the JS SDK holds every JSON number: a literal
// beyond float64 is ±Infinity, as in JavaScript. Any other value, and a
// json.Number that is not a JSON number literal ("NaN", "Inf"), is returned
// unchanged.
func numberValue(x reflect.Value) reflect.Value {
	if x.IsValid() && x.Type() == jsonNumberType {
		var text = x.String()
		if len(text) > 0 && (text[0] == '-' || text[0] >= '0' && text[0] <= '9') && json.Valid([]byte(text)) {
			// A range error still returns the ±Inf the literal rounds to.
			var f, _ = strconv.ParseFloat(text, 64)
			return reflect.ValueOf(f)
		}
	}
	return x
}

func unwrap(v reflect.Value) reflect.Value {
	for v.IsValid() && (v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer) {
		if v.IsNil() {
			return reflect.Value{}
		}
		v = v.Elem()
	}
	return v
}

func isCollection(v reflect.Value) bool {
	v = unwrap(v)
	return v.IsValid() && (v.Kind() == reflect.Slice || v.Kind() == reflect.Array || v.Kind() == reflect.Map)
}

func toFloat(v reflect.Value) (float64, bool) {
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(v.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(v.Uint()), true
	case reflect.Float32, reflect.Float64:
		return v.Float(), true
	}
	return 0, false
}

func deepEquals(lhs reflect.Value, rhs reflect.Value) bool {
	lhs = numberValue(unwrap(lhs))
	rhs = numberValue(unwrap(rhs))
	if !lhs.IsValid() || !rhs.IsValid() {
		return !lhs.IsValid() && !rhs.IsValid()
	}

	if lf, ok := toFloat(lhs); ok {
		rf, rok := toFloat(rhs)
		return rok && (lf == rf || (math.IsNaN(lf) && math.IsNaN(rf)))
	}

	switch lhs.Kind() {
	case reflect.Slice, reflect.Array:
		if rhs.Kind() != reflect.Slice && rhs.Kind() != reflect.Array {
			return false
		}
		if lhs.Len() != rhs.Len() {
			return false
		}
		for i := 0; i < lhs.Len(); i++ {
			if !deepEquals(lhs.Index(i), rhs.Index(i)) {
				return false
			}
		}
		return true
	case reflect.Map:
		if rhs.Kind() != reflect.Map || lhs.Len() != rhs.Len() {
			return false
		}
		iter := lhs.MapRange()
		for iter.Next() {
			key := iter.Key()
			if !key.Type().AssignableTo(rhs.Type().Key()) {
				return false
			}
			rv := rhs.MapIndex(key)
			if !rv.IsValid() || !deepEquals(iter.Value(), rv) {
				return false
			}
		}
		return true
	}

	if lhs.Kind() != rhs.Kind() {
		return false
	}
	if lhs.Comparable() && rhs.Comparable() {
		return lhs.Interface() == rhs.Interface()
	}
	return reflect.DeepEqual(lhs.Interface(), rhs.Interface())
}
