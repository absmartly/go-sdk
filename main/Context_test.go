package main

import "testing"

var units = map[string]string{
	"session_id": "e791e240fcd3df7d238cfc285f475e8152fcc0ec",
	"user_id":    "123456789",
	"email":      "bleh@absmartly.com"}

func TestConstructorSetsCustomAssignments(t *testing.T) {
	var overrides = map[string]int{"exp_test": 2, "exp_test_1": 1}
	var config = ContextConfig{}
	config.Units_ = units
	config.Overrides_ = overrides

	//var context = CreateContext()
	//assert(5, result.(int), t)
}
