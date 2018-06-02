package selfFastHttp

/*
结论: 除非追求极限性能,否则无需引用此方法,time.Now()足够用
goos: linux
goarch: amd64
pkg: testBench
BenchmarkCoarseTimeNow-8        2000000000               1.07 ns/op
BenchmarkNow-8                  2000000000               0.68 ns/op
BenchmarkTimeNow-8              200000000                9.89 ns/op
BenchmarkTimeNowTruncate-8      100000000               15.8 ns/op
PASS
ok      testBench       8.259s

goos: windows
goarch: amd64
pkg: testBench
BenchmarkCoarseTimeNow-8     	2000000000	         1.09 ns/op
BenchmarkNow-8               	1000000000	         3.11 ns/op
BenchmarkTimeNow-8           	1000000000	         2.24 ns/op
BenchmarkTimeNowTruncate-8   	200000000	         7.77 ns/op
PASS
ok  	testBench	10.663s
*/

import (
	"sync/atomic"
	"time"
	"unsafe"
)

/*
//old
func CoarseTimeNow() time.Time {
	tp := coarseTime.Load().(*time.Time)
	return *tp
}

func initCoarseTime() {
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
*/
//=====================speedup 3 times(0.4ns 1.2ns)

//var gInit sync.Once
func init() {
	// 不使用
	//	gInit.Do(func() {
	t := time.Now().Truncate(time.Second)
	gTime = &t
	go func() {
		for {
			time.Sleep(time.Second)
			t := time.Now().Truncate(time.Second)
			pt := &t
			atomic.StorePointer((*unsafe.Pointer)(unsafe.Pointer(&gTime)), unsafe.Pointer(pt))
		}
	}()
	//	})
}

// CoarseTimeNow returns the current time truncated to the nearest second.
//
// This is a faster alternative to time.Now().
func CoarseTimeNow() time.Time {
	//if gTime == nil {
	//	//fmt.Println(gTime, " => nil")
	//	return time.Now()
	//}
	val := atomic.LoadPointer(((*unsafe.Pointer)(unsafe.Pointer(&gTime))))
	pt := (*time.Time)(val)
	return *pt
}

var gTime *time.Time
