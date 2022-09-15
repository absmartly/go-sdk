package main

type MapperInt interface {
	Apply(value interface{}) interface{}
}
