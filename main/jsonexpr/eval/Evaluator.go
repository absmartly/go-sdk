package eval

import (
	"errors"
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
		entry.Next()
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

	if (lhs.Kind() == reflect.Slice || lhs.Kind() == reflect.Array || lhs.Kind() == reflect.Map) && lhs.Kind() == rhs.Kind() {
		if lhs.Len() > rhs.Len() {
			return 1
		} else if rhs.Len() > lhs.Len() {
			return -1
		}

		if lhs.Kind() == reflect.Slice || lhs.Kind() == reflect.Array {
			for i := 0; i < lhs.Len(); i++ {
				if lhs.Index(i).Interface() != rhs.Index(i).Interface() {
					return nil
				}
			}
			return 0
		} else if lhs.Kind() == reflect.Map {
			var entry = lhs.MapRange()
			var rentry = rhs.MapRange()
			for entry.Next() {
				rentry.Next()
				if entry.Key().Interface() != rentry.Key().Interface() ||
					entry.Value().Interface() != rentry.Value().Interface() {
					return nil
				}
			}
			return 0
		}
	}

	if lhs.IsValid() && rhs.IsValid() && lhs.Kind() == rhs.Kind() && lhs == rhs {
		if lhs.IsNil() && rhs.IsNil() {
			return nil
		}
		return 0
	}

	if !lhs.IsValid() && !rhs.IsValid() {
		return 0
	}

	return nil
}

func (e Evaluator) BooleanConvert(x reflect.Value) bool {
	if x.Kind() == reflect.Bool {
		return x.Bool()
	} else if x.Kind() == reflect.String {
		var str = x.String()
		return str != "false" && str != "0" && str != ""
	} else if x.Kind() == reflect.Int || x.Kind() == reflect.Float64 {
		return x.Int() != 0
	} else if x.IsValid() && (x.Interface() == false || x.Interface() == "false" || x.Interface() == "[false]") {
		return false
	} else if x.IsValid() && (x.Interface() == true || x.Interface() == "true" || x.Interface() == "[true]") {
		return true
	} else {
		return x.IsValid() && !x.IsNil()
	}
}

func (e Evaluator) NumberConvert(x reflect.Value) (float64, error) {
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
