package selfFastHttp

//服务池：chan池,控制并发协程池数

import (
	"net"
	"runtime"
	"strings"
	"sync"
	"time"
)

//限制最大数、超时管理、可记日志的chan池
type workerPool struct {
	ServeFunc func(c net.Conn) error //自定义的处理接口

	MaxWorkersCount int //最大chan池数量

	LogAllErrors bool // 是否记录所有错误

	MaxIdleWorkerDuration time.Duration // 最大闲置时间

	Logger Logger

	//private ---------
	lock         sync.Mutex
	workersCount int
	isMustStop   bool

	ready []*workerChan //闲置chan池,FILO

	stopCh chan struct{}

	workerChanPool sync.Pool
}

//含有效时间的chan
type workerChan struct {
	lastUseTime time.Time
	ch          chan net.Conn
}

func (wp *workerPool) getMaxIdleWorkerDuration() time.Duration {
	if wp.MaxIdleWorkerDuration <= 0 {
		return 10 * time.Second
	}
	return wp.MaxIdleWorkerDuration
}

//开始运行
//启用协程，定时处理闲置chan
func (wp *workerPool) Start() {
	if wp.stopCh != nil {
		panic("Bug: workerPool already started")
	}
	if wp.ServeFunc == nil {
		panic("Bug: workerPool.ServeFunc not inited")
	}
	wp.stopCh = make(chan struct{})
	stopCh := wp.stopCh

	//1.清理闲置超时的chan
	//2.受stopCh控制
	//3.间隔时间
	go func() {
		var scratch []*workerChan //直接定义heap一个变量,而不用后续持续create
		for {
			wp.clean(&scratch)
			select {
			case <-stopCh:
				return
			default:
				time.Sleep(wp.getMaxIdleWorkerDuration())
			}
		}
	}()
}

//清理闲置chan
func (wp *workerPool) clean(scratch *[]*workerChan) {
	maxIdleWorkerDuration := wp.getMaxIdleWorkerDuration()

	now := time.Now()

	wp.lock.Lock()
	ready := wp.ready
	n := len(ready)
	i := 0
	for i < n && now.Sub(ready[i].lastUseTime) > maxIdleWorkerDuration {
		i++
	}
	*scratch = append((*scratch)[:0], ready[:i]...)

	if i > 0 {
		m := copy(ready, ready[i:])
		for i = m; i < n; i++ {
			ready[i] = nil
		}
		wp.ready = ready[:m]
	}
	wp.lock.Unlock()

	tmp := *scratch
	for i, ch := range tmp {
		ch.ch <- nil // 阻塞处理，业务耗时不可控
		tmp[i] = nil
	}
}

//1.添加停止标志
//2.清理闲置chan
func (wp *workerPool) Stop() {
	if wp.stopCh == nil {
		panic("Bug: workerPool wasn't started")
	}
	close(wp.stopCh) //触发select
	wp.stopCh = nil

	//清理闲置chan池
	wp.lock.Lock()
	ready := wp.ready
	for i, ch := range ready {
		ch.ch <- nil
		ready[i] = nil
	}
	wp.ready = ready[:0]
	wp.isMustStop = true
	wp.lock.Unlock()
}

//对外服务
func (wp *workerPool) Serve(c net.Conn) bool {
	ch := wp.getCh()
	if ch == nil {
		return false
	}
	ch.ch <- c
	return true
}

var workerChanGap = func() int {
	//据说明：单核阻塞更好 go1.5
	if runtime.GOMAXPROCS(0) == 1 {
		return 0
	}

	return 1
}()

//取一个chan
func (wp *workerPool) getCh() *workerChan {
	var ch *workerChan
	createWorker := false

	wp.lock.Lock()
	if wp.isMustStop {
		wp.lock.Unlock()
		return nil
	}
	ready := wp.ready
	n := len(ready) - 1
	if n < 0 {
		if wp.workersCount < wp.MaxWorkersCount {
			createWorker = true
			wp.workersCount++
		}
	} else {
		ch = ready[n]
		ready[n] = nil
		wp.ready = ready[:n]
	}
	wp.lock.Unlock()

	if createWorker {
		vch := wp.workerChanPool.Get()
		if vch == nil {
			vch = &workerChan{
				ch: make(chan net.Conn, workerChanGap),
			}
		}
		ch = vch.(*workerChan)
		go func() {
			wp.workerFunc(ch)
			wp.workerChanPool.Put(vch)
		}()
	}

	return ch
}

func (wp *workerPool) release(ch *workerChan) bool {
	wp.lock.Lock()
	if wp.isMustStop {
		wp.lock.Unlock()
		return false
	}
	ch.lastUseTime = time.Now() //CoarseTimeNow()
	wp.ready = append(wp.ready, ch)
	wp.lock.Unlock()
	return true
}

func (wp *workerPool) workerFunc(ch *workerChan) {
	var c net.Conn

	var err error
	for c = range ch.ch {
		if c == nil {
			break
		}

		if err = wp.ServeFunc(c); err != nil && err != errHijacked {
			errStr := err.Error()
			if wp.LogAllErrors || !(strings.Contains(errStr, "broken pipe") ||
				strings.Contains(errStr, "reset by peer") ||
				strings.Contains(errStr, "i/o timeout")) {
				wp.Logger.Printf("error when serving connection %q<->%q: %s", c.LocalAddr(), c.RemoteAddr(), errStr)
			}
		}
		if err != errHijacked {
			c.Close() //todo close conn
		}
		c = nil

		if !wp.release(ch) { //failed while stop
			break
		}
	}

	wp.lock.Lock()
	wp.workersCount--
	wp.lock.Unlock()
}
