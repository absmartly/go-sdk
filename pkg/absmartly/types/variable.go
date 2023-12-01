package types

import "encoding/json"

type Variable struct {
	raw []byte
	v   interface{}
	i   int
	s   string
	b   bool
}

func NewVariable(value interface{}) Variable {
	v := Variable{v: value}
	v.raw, _ = json.Marshal(value)
	switch val := value.(type) {
	case int, int8, int16, int32:
		v.i = value.(int)
	case uint, uint8, uint16, uint32:
		v.i = value.(int)
	case bool:
		v.b = val
	case string:
		v.s = val
	}

	return v
}

func (v *Variable) Int() int {
	return v.i
}

func (v *Variable) String() string {
	return v.s
}

func (v *Variable) Bool() bool {
	return v.b
}

func (v *Variable) Interface() interface{} {
	return v.v
}

func (v *Variable) UnmarshalJSON(bytes []byte) error {
	copy(v.raw, bytes)
	err := json.Unmarshal(bytes, &v.v)
	if err != nil {
		return err
	}
	_ = json.Unmarshal(bytes, &v.i)
	_ = json.Unmarshal(bytes, &v.s)
	_ = json.Unmarshal(bytes, &v.b)

	return nil
}
