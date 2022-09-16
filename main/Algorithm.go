package main

func MapSetToArray(set []interface{}, array []interface{}, mapper MapperInt) interface{} {
	var size = len(set)
	if len(array) < size {
		array = make([]interface{}, size)
	}

	if len(array) > size {
		array[size] = nil
	}

	var index = 0
	for key := range set {
		array[index] = mapper.Apply(key)
		index++
	}

	return array
}
