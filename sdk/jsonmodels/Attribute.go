package jsonmodels

type Attribute struct {
	Name  string      `json:"name"`
	Value interface{} `json:"value,omitempty,"`
	SetAt int64       `json:"setAt"`
}
