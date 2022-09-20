package main

import (
	"sync"
	"testing"
)

var rwLock sync.RWMutex

type Computer struct {
	MapperInt
}

func (c Computer) Apply(value interface{}) interface{} {
	if value == 1 {
		return 5
	} else {
		return value
	}
}

func TestComputeIfAbsentRW(t *testing.T) {
	var mp = map[interface{}]interface{}{}
	var computer = Computer{}
	var result = ComputeIfAbsentRW(&rwLock, true, mp, 1, computer)
	assert(5, result.(int), t)
}

type ComputerSecond struct {
	MapperInt
}

func TestComputeIfAbsentRWPresent(t *testing.T) {
	var mp = map[interface{}]interface{}{1: 5}
	var computer = ComputerSecond{}
	var result = ComputeIfAbsentRW(&rwLock, true, mp, 1, computer)
	assert(5, result.(int), t)
}

func TestComputeIfAbsentRWPresentAfterLock(t *testing.T) {
	var mp = map[interface{}]interface{}{}
	var computer = Computer{}
	var result = ComputeIfAbsentRW(&rwLock, true, mp, 1, computer)
	assert(5, result.(int), t)
}

func TestGetRW(t *testing.T) {
	var mp = map[interface{}]interface{}{}
	var result = GetRW(&rwLock, mp, 1)
	assertAny(nil, result, t)
}

func TestPutRW(t *testing.T) {
	var mp = map[interface{}]interface{}{}
	var result = PutRW(&rwLock, mp, 1, 5)
	assertAny(5, result, t)
}

func TestAddRW(t *testing.T) {
	var mp = []interface{}{5}
	var result = AddRW(&rwLock, mp, 5)
	assertAny([]interface{}{5, 5}, result, t)
}
