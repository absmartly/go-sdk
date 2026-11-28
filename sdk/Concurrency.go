package sdk

import (
	"sync"
	"sync/atomic"
)

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
		defer lock.Unlock()
		value, exist = maps[key]
		if exist {
			return value
		}
		var newValue = computer.Apply(key)
		maps[key] = newValue
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

// AddRW appends under lock. The caller must assign the returned slice header.
func AddRW(lock *sync.RWMutex, list []interface{}, value interface{}) interface{} {
	lock.Lock()
	list = append(list, value)
	lock.Unlock()
	return list
}

// atomicFlag is a boolean that is read and written atomically. The module's
// floor is Go 1.17, before sync/atomic had atomic.Bool (Go 1.19).
type atomicFlag struct{ v int32 }

func flagValue(value bool) int32 {
	if value {
		return 1
	}
	return 0
}

func (f *atomicFlag) Load() bool { return atomic.LoadInt32(&f.v) != 0 }

func (f *atomicFlag) Store(value bool) { atomic.StoreInt32(&f.v, flagValue(value)) }

// Swap stores value and returns the previous value.
func (f *atomicFlag) Swap(value bool) bool { return atomic.SwapInt32(&f.v, flagValue(value)) != 0 }
