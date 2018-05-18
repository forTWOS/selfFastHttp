package selfFastHttp

import (
	"sync"
	"sync/atomic"
	"time"
)

func CoarseTimeNow() time.Time {
	tp := coarseTime.Load().(*time.Time)
	return *tp
}

func init() {
	t := time.Now().Truncate(time.Second)
	coarseTime.Store(&t)
	go func() {
		for {
			time.Sleep(time.Second)
			t := time.Now().Truncate(time.Second)
			coarseTime.Store(&t)
		}
	}()
}

var tPool sync.Pool
var coarseTime atomic.Value
