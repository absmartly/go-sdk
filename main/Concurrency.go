package main

import "sync"

func ComputeIfAbsentRW(lock *sync.RWMutex, maps map[interface{}]interface{}, key interface{}, computer MapperInt) interface{} {
	lock.RLock()
	var value = maps[key]
	if value != nil {
		lock.RUnlock()
		return value
	}
	lock.RUnlock()

	lock.Lock()
	var newValue = computer.Apply(key)
	maps[key] = newValue
	lock.Unlock()
	return newValue
}

func GetRW(lock *sync.RWMutex, maps map[interface{}]interface{}, key interface{}) interface{} {
	lock.RLock()
	var value = maps[key]
	lock.RUnlock()
	return value
}

func PutRW(lock *sync.RWMutex, maps map[interface{}]interface{}, key interface{}, value interface{}) interface{} {
	lock.Lock()
	maps[key] = value
	lock.Unlock()
	return value
}

func AddRW(lock *sync.RWMutex, list []interface{}, value interface{}) interface{} {
	lock.Lock()
	var result = append(list, value)
	lock.Unlock()
	return result
}
