package types

import (
	"errors"
	"fmt"
	"strconv"
)

var (
	ErrUnknownType = errors.New("unknown custom field type")
	ErrConversion  = errors.New("value conversion error")
)

type fType uint8

const (
	fTypeZero   fType = iota
	fTypeString fType = iota
	fTypeInt    fType = iota
	fTypeJSON   fType = iota
	fTypeBool   fType = iota
)

type Field struct {
	value interface{}
	t     fType
}

func EmptyField() Field {
	return Field{t: fTypeZero}
}

func NewField(value string, t string) (Field, error) {
	f := Field{}
	switch t {
	case "text":
		fallthrough
	case "string":
		f.t = fTypeString
		f.value = value
	case "number":
		v, err := strconv.Atoi(value)
		if err != nil {
			return f, fmt.Errorf("%w: %v", ErrConversion, err)
		}
		f.value = v
		f.t = fTypeInt
	case "json":
		f.t = fTypeJSON
		f.value = value
		// todo JSON
	case "boolean":
		v, err := strconv.ParseBool(value)
		if err != nil {
			return f, fmt.Errorf("%w: %v", ErrConversion, err)
		}
		f.value = v
		f.t = fTypeBool
	default:
		return f, fmt.Errorf(
			"%w '%s' you may need to upgrade to the latest SDK version",
			ErrUnknownType,
			t,
		)
	}

	return f, nil
}

func (f Field) ValueInterface() interface{} {
	return f.value
}

func (f Field) ValueInt() int {
	if f.t == fTypeInt {
		return f.value.(int)
	}

	return 0
}

func (f Field) ValueString() string {
	if f.t == fTypeString {
		return f.value.(string)
	}

	return ""
}

func (f Field) ValueBool() bool {
	if f.t == fTypeBool {
		return f.value.(bool)
	}

	return false
}

// todo JSON
