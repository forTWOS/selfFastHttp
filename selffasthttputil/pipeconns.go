package selffasthttputil

import (
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

// 双向联通管道
func NewPipeConns() *PipeConns {
	ch1 := make(chan *byteBuffer, 4)
	ch2 := make(chan *byteBuffer, 4)

	pc := &PipeConns{
		stopCh: make(chan struct{}),
	}
	pc.c1.rCh = ch1
	pc.c1.wCh = ch2
	pc.c2.rCh = ch2
	pc.c2.wCh = ch1
	pc.c1.pc = pc
	pc.c2.pc = pc
	return pc
}

// 提供双向联通管道:使用进程内的内存，作为联结
// 该类型，须通过NewPipeConns创建
// 与net.Pipe连接相比，有以下属性:
// * 更快
// * 缓存写调用,所以无需并发协程，去调用 读接口，以便无阻塞所有 Write接口
// * 支持 读/写超时
type PipeConns struct {
	c1         pipeConn
	c2         pipeConn
	stopCh     chan struct{}
	stopChLock sync.Mutex
}

// 返回首端-双向通道
// 数据写入Conn1，有可能从Conn2读
// 数据写入Conn2,有可能从Conn1写
func (pc *PipeConns) Conn1() net.Conn {
	return &pc.c1
}

func (pc *PipeConns) Conn2() net.Conn {
	return &pc.c2
}

// 关闭双向管道
func (pc *PipeConns) Close() error {
	pc.stopChLock.Lock()
	select {
	case <-pc.stopCh: //检测是否已关闭
	default:
		close(pc.stopCh)
	}
	pc.stopChLock.Unlock()

	return nil
}

type pipeConn struct {
	b  *byteBuffer
	bb []byte

	rCh chan *byteBuffer //读通道
	wCh chan *byteBuffer //写通道
	pc  *PipeConns

	readDeadlineTimer  *time.Timer //读超时器
	writeDeadlineTimer *time.Timer //写超时器

	readDeadlineCh  <-chan time.Time // 读超时信号器
	writeDeadlineCh <-chan time.Time // 写超时信号器
}

// 实现io.Write接口
func (c *pipeConn) Write(p []byte) (int, error) {
	b := acquireByteBuffer()
	b.b = append(b.b[:0], p...) //初始化b.b.，将p放入缓冲区

	select {
	case <-c.pc.stopCh: // 双向通道关闭
		releaseByteBuffer(b)
		return 0, errConnectionClosed
	default:
	}

	select {
	case c.wCh <- b: // 传数据到写通道
	default: // 若无法传入(缓存区满)
		select {
		case c.wCh <- b: // 传数据到写通道
		case <-c.writeDeadlineCh: // 写超时
			c.writeDeadlineCh = closedDeadlineCh
			return 0, ErrTimeout
		case <-c.pc.stopCh: // 双向通道关闭
			releaseByteBuffer(b)
			return 0, errConnectionClosed
		}
	}

	return len(p), nil
}

// --- Read
func (c *pipeConn) Read(p []byte) (int, error) {
	mayBlock := true //首次阻塞取数据
	nn := 0
	for len(p) > 0 {
		n, err := c.read(p, mayBlock)
		nn += n
		if err != nil {
			if !mayBlock && err == errWouldBlock { //取数据需要阻塞
				err = nil
			}
			return nn, err
		}
		p = p[n:]
		mayBlock = false // 除第一次，后续取数据，不阻塞;循环取数据
	}

	return nn, nil
}

func (c *pipeConn) read(p []byte, mayBlock bool) (int, error) {
	if len(c.bb) == 0 { // 无数据，去读
		if err := c.readNextByteBuffer(mayBlock); err != nil {
			return 0, err
		}
	}
	n := copy(p, c.bb)
	c.bb = c.bb[n:]

	return n, nil
}

func (c *pipeConn) readNextByteBuffer(mayBlock bool) error {
	releaseByteBuffer(c.b)
	c.b = nil

	select {
	case c.b = <-c.rCh:
	default:
		if !mayBlock {
			return errWouldBlock
		}
		// 阻塞处理
		select {
		case c.b = <-c.rCh:
		case <-c.readDeadlineCh: // 读超时
			c.readDeadlineCh = closedDeadlineCh
			// 在超时时，rCh有可能已读取到数据，需处理之
			select {
			case c.b = <-c.rCh:
			default:
				return ErrTimeout
			}
		case <-c.pc.stopCh: // 双向通道关闭
			// 在超时时，rCh有可能已读取到数据，需处理之
			select {
			case c.b = <-c.rCh:
			default:
				return io.EOF
			}
		}
	}

	c.bb = c.b.b
	return nil
}

var (
	errWouldBlock       = errors.New("would block")
	errConnectionClosed = errors.New("connection closed")

	// 读/写 超时
	ErrTimeout = errors.New("timeout")
)

func (c *pipeConn) Close() error {
	return c.pc.Close()
}

func (c *pipeConn) LocalAddr() net.Addr {
	return pipeAddr(0)
}

func (c *pipeConn) RemoteAddr() net.Addr {
	return pipeAddr(0)
}

func (c *pipeConn) SetDeadline(deadline time.Time) error {
	c.SetReadDeadline(deadline)
	c.SetWriteDeadline(deadline)
	return nil
}

func (c *pipeConn) SetReadDeadline(deadline time.Time) error {
	if c.readDeadlineTimer == nil {
		c.readDeadlineTimer = time.NewTimer(time.Hour)
	}
	c.readDeadlineCh = updateTimer(c.readDeadlineTimer, deadline)
	return nil
}
func (c *pipeConn) SetWriteDeadline(deadline time.Time) error {
	if c.writeDeadlineTimer == nil {
		c.writeDeadlineTimer = time.NewTimer(time.Hour)
	}
	c.writeDeadlineCh = updateTimer(c.writeDeadlineTimer, deadline)
	return nil
}
func updateTimer(t *time.Timer, deadline time.Time) <-chan time.Time {
	if !t.Stop() { //清空
		select {
		case <-t.C:
		default:
		}
	}
	if deadline.IsZero() { //检测0值
		return nil
	}
	d := -time.Since(deadline) //时间的间隔
	if d <= 0 {                //已过期
		return closedDeadlineCh
	}
	t.Reset(d)
	return t.C
}

var closedDeadlineCh = func() <-chan time.Time { //关闭的时间管道
	ch := make(chan time.Time)
	close(ch)
	return ch
}()

type pipeAddr int

func (pipeAddr) Network() string {
	return "pipe"
}
func (pipeAddr) String() string {
	return "pipe"
}

type byteBuffer struct {
	b []byte
}

func acquireByteBuffer() *byteBuffer {
	return byteBufferPool.Get().(*byteBuffer)
}
func releaseByteBuffer(b *byteBuffer) {
	if b != nil {
		byteBufferPool.Put(b)
	}
}

var byteBufferPool = &sync.Pool{
	New: func() interface{} {
		return &byteBuffer{
			b: make([]byte, 1024),
		}
	},
}
