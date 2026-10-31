package collector

import (
	"sync"
	"time"
)

// timeNow is the clock for gauges derived from the wall clock: node uptime,
// peer last-seen, mempool age and collection duration. Tests replace it
// through SetNowForTest so a golden /metrics snapshot stays stable.
var (
	timeNowMu sync.Mutex
	timeNow   = time.Now
)

func clockNow() time.Time {
	timeNowMu.Lock()
	fn := timeNow
	timeNowMu.Unlock()

	return fn()
}

// SetNowForTest replaces the clock used for time-derived gauges.
// Passing nil restores time.Now. The returned function restores the previous
// clock. Production callers leave the clock on time.Now.
func SetNowForTest(fn func() time.Time) (restore func()) {
	timeNowMu.Lock()
	prev := timeNow

	if fn == nil {
		timeNow = time.Now
	} else {
		timeNow = fn
	}

	timeNowMu.Unlock()

	return func() {
		timeNowMu.Lock()
		timeNow = prev
		timeNowMu.Unlock()
	}
}
