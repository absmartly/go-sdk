package main

import "sync"

func ComputeIfAbsentRW(lock *sync.RWMutex, needlock bool, maps map[interface{}]interface{}, key interface{}, computer MapperInt) interface{} {
	if needlock {
		lock.RLock()
		var value, exist = maps[key]
		if exist {
			lock.RUnlock()
			return value
		}
		lock.RUnlock()

		lock.Lock()
		var newValue = computer.Apply(key)
		maps[key] = newValue
		lock.Unlock()
		return newValue
	} else {
		var value, exist = maps[key]
		if exist {
			return value
		}

		var newValue = computer.Apply(key)
		maps[key] = newValue
		return newValue
	}
}

func GetRW(lock *sync.RWMutex, maps map[interface{}]interface{}, key interface{}) interface{} {
	lock.RLock()
	var value, exist = maps[key]
	if exist {
		lock.RUnlock()
		return value
	} else {
		lock.RUnlock()
		return nil
	}
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
