package internal

import "time"

type Clock interface {
	Millis() int64
}

type SystemClockUTC struct {
	Clock
}

func (s SystemClockUTC) millis() int64 {
	return time.Now().UnixNano() / int64(time.Millisecond)
}

type FixedClock struct {
	Clock
	millis_ int64
}

func (f FixedClock) millis() int64 {
	return f.millis_
}
