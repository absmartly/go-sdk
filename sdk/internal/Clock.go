package internal

import "time"

type Clock interface {
	Millis() int64
}

type SystemClockUTC struct {
	Clock
}

func (s SystemClockUTC) Millis() int64 {
	return time.Now().UnixNano() / int64(time.Millisecond)
}

type FixedClock struct {
	Clock
	Millis_ int64
}

func (f FixedClock) Millis() int64 {
	return f.Millis_
}
