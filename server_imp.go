package selfFastHttp

import (
	"bufio"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

func (s *Server) ListenAndServe(addr string) error {
	ln, err := net.Listen("tcp4", addr)
	if err != nil {
		return err
	}
	return s.Serve(ln)
}

// 先移除 该地址上的文件  todo??
func (s *Server) ListenAndServeUNIX(addr string, mode os.FileMode) error {
	if err := os.Remove(addr); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("unexpected error when trying to remove unix socket file %q: %s", addr, err)
	}
	ln, err := net.Listen("unix", addr)
	if err != nil {
		return err
	}
	if err = os.Chmod(addr, mode); err != nil {
		return fmt.Errorf("cannot chmod %#o for %q: %s", mode, addr, err)
	}
	return s.Serve(ln)
}

// HTTPS requests
func (s *Server) ListenAndServeTLS(addr, certFile, keyFile string) error {
	ln, err := net.Listen("tcp4", addr)
	if err != nil {
		return err
	}
	return s.ServeTLS(ln, certFile, keyFile)
}

// HTTPS requests
func (s *Server) ListenAndServeTLSEmbed(addr string, certData, keyData []byte) error {
	ln, err := net.Listen("tcp4", addr)
	if err != nil {
		return err
	}
	return s.ServeTLSEmbed(ln, certData, keyData)
}

func (s *Server) ServeTLS(ln net.Listener, certFile, keyFile string) error {
	lnTLS, err := newTLSListener(ln, certFile, keyFile) //嵌套tls
	if err != nil {
		return err
	}
	return s.Serve(lnTLS)
}
func (s *Server) ServeTLSEmbed(ln net.Listener, certData, keyData []byte) error {
	lnTLS, err := newTLSListenerEmbed(ln, certData, keyData)
	if err != nil {
		return err
	}
	return s.Serve(lnTLS)
}
func newTLSListener(ln net.Listener, certFile, keyFile string) (net.Listener, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("cannot load TLS key pair from certFile=%q and keyFile=%q: %s", certFile, keyFile, err)
	}
	return newCertListener(ln, &cert), nil
}
func newTLSListenerEmbed(ln net.Listener, certData, keyData []byte) (net.Listener, error) {
	cert, err := tls.X509KeyPair(certData, keyData)
	if err != nil {
		return nil, fmt.Errorf("cannot load TLS key pair from the provided certData(%d) and keyData(%d): %s",
			len(certData), len(keyData), err)
	}
	return newCertListener(ln, &cert), nil
}
func newCertListener(ln net.Listener, cert *tls.Certificate) net.Listener {
	tlsConfig := &tls.Config{
		Certificates:             []tls.Certificate{*cert},
		PreferServerCipherSuites: true, // 设为true,Server按顺序使用Certificates里证书
	}
	return tls.NewListener(ln, tlsConfig)
}

// 一个server并发数
const DefaultConcurrency = 256 * 1024 //略少

func (s *Server) Serve(ln net.Listener) error {
	var lastOverflowErrorTime time.Time // 用于记录上次满载时间，以便计算两次出错间隔，若超过1分钟，打印日志
	var lastPerIPErrorTime time.Time
	var c net.Conn
	var err error

	maxWorkersCount := s.getConcurrency()
	s.concurrencyCh = make(chan struct{}, maxWorkersCount)
	wp := &workerPool{
		ServeFunc:       s.serveConn,
		MaxWorkersCount: maxWorkersCount,
		LogAllErrors:    s.LogAllErrors,
		Logger:          s.logger(),
	}
	wp.Start()

	for {
		if c, err = acceptConn(s, ln, &lastPerIPErrorTime); err != nil {
			wp.Stop() // 出现错误，停止服务
			if err == io.EOF {
				return nil
			}
			return err
		}
		if !wp.Serve(c) {
			s.writeFastError(c, StatusServiceUnavailable,
				"The connection cannot be served because Server.Concurrency limit exceeded")
			c.Close()
			if time.Since(lastOverflowErrorTime) > time.Minute { // 超过1分钟还是满载情况，打印日志
				s.logger().Printf("The incoming connection cannot be served, because %d concurrent connections are served. "+
					"Try increasing Server.Concurrency", maxWorkersCount)
				lastOverflowErrorTime = time.Now()
			}

			// 当前服务器已满载，给其它服务器机会
			// 希望其它服务器没有满载
			time.Sleep(100 * time.Millisecond)
		}
		c = nil
	}
}

// 阻塞获取连接
func acceptConn(s *Server, ln net.Listener, lastPerIPErrorTime *time.Time) (net.Conn, error) {
	for {
		c, err := ln.Accept()
		if err != nil {
			if c != nil {
				panic("BUG: net.Listener returned non-nil conn and non-nil error")
			}
			if netErr, ok := err.(net.Error); ok && netErr.Temporary() { // 临时错误
				s.logger().Printf("Temporary error when accepting new connections: %s", netErr)
				time.Sleep(time.Second)
				continue
			}
			if err != io.EOF && !strings.Contains(err.Error(), "use of closed network connection") {
				s.logger().Printf("Permanent error when accepting new connections: %s", err)
				return nil, err
			}
			return nil, io.EOF
		}
		if c == nil {
			panic("BUG: net.Listener returned (nil, nil)")
		}
		if s.MaxConnsPerIP > 0 {
			pic := wrapPerIPConn(s, c)
			if pic == nil {
				if time.Since(*lastPerIPErrorTime) > time.Minute {
					s.logger().Printf("The number of connections from %s exceeds MaxConnsPerIP=%d",
						getConnIP4(c), s.MaxConnsPerIP)
					*lastPerIPErrorTime = time.Now()
				}
				continue
			}
			c = pic
		}
		//		s.logger().Printf("[Accept] new conn:%d", c)
		return c, nil
	}
}

// 检测该ip上的连接数是否超了
func wrapPerIPConn(s *Server, c net.Conn) net.Conn {
	ip := getUint32IP(c)
	if ip == 0 {
		return c
	}
	n := s.perIPConnCounter.Register(ip)
	if n > s.MaxConnsPerIP {
		s.perIPConnCounter.Unregister(ip)
		s.writeFastError(c, StatusTooManyRequests, "The number of connections from your ip exceeds MaxConnsPerIP")
		c.Close()
		return nil
	}
	return acquirePerIPConn(c, ip, &s.perIPConnCounter)
}

var defaultLogger = Logger(log.New(os.Stderr, "", log.LstdFlags))

//取日志imp
func (s *Server) logger() Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return defaultLogger
}

var (
	ErrPerIPConnLimit   = errors.New("too many connections per ip")
	ErrConcurrencyLimit = errors.New("cannot serve the connection because Server.Concurrency concurrent connections are served")
	ErrKeepaliveTimeout = errors.New("exceeded MaxKeepaliveDuration")
)

// 处理c请求:前置检测-maxConnPerIP,concurrency
// 请求成功，返回nil,否则返回error
// c须立即将响应数据传到Write中，否则请求处理，会卡住
// c会在返回前Close
func (s *Server) ServeConn(c net.Conn) error {
	if s.MaxConnsPerIP > 0 {
		pic := wrapPerIPConn(s, c)
		if pic == nil {
			return ErrPerIPConnLimit
		}
		c = pic
	}

	n := atomic.AddUint32(&s.concurrency, 1)
	if n > uint32(s.getConcurrency()) {
		atomic.AddUint32(&s.concurrency, ^uint32(0)) // -1
		s.writeFastError(c, StatusServiceUnavailable, "The connection cannot be served because Server.Concurrency limit exceeded")
		c.Close()
		return ErrConcurrencyLimit
	}

	err := s.serveConn(c)

	atomic.AddUint32(&s.concurrency, ^uint32(0)) // -1

	if err != errHijacked {
		err1 := c.Close()
		if err == nil {
			err = err1
		}
	} else {
		err = nil
	}
	return err
}

var errHijacked = errors.New("connection has been hijacked")

//取得最大并发数
func (s *Server) getConcurrency() int {
	n := s.Concurrency
	if n <= 0 {
		n = DefaultConcurrency
	}
	return n
}

var globalConnID uint64

func nextConnID() uint64 {
	return atomic.AddUint64(&globalConnID, 1)
}

// DefaultMaxRequestBodySize is the maximum request body size the server
// reads by default.
//
// See Server.MaxRequestBodySize for details.
const DefaultMaxRequestBodySize = 4 * 1024 * 1024 // 4M

// 具体http服务处理
// 没有引用c即可
func (s *Server) serveConn(c net.Conn) error {
	serverName := s.getServerName()
	connRequestNum := uint64(0)
	connID := nextConnID()
	currentTime := time.Now()
	connTime := currentTime
	maxRequestBodySize := s.MaxRequestBodySize
	if maxRequestBodySize <= 0 {
		maxRequestBodySize = DefaultMaxRequestBodySize
	}

	ctx := s.acquireCtx(c) // 产生RequestCtx
	ctx.connTime = connTime
	isTLS := ctx.IsTLS()
	var (
		br *bufio.Reader // 读-获取请求数据
		bw *bufio.Writer // 写-响应数据

		err             error
		timeoutResponse *Response     // 超时响应
		hijackHandler   HijackHandler // 劫持处理接口

		lastReadDeadlineTime  time.Time // 上次读超时时间
		lastWriteDeadlineTime time.Time // 上次写超时时间

		connectionClose bool
		isHTTP11        bool
	)
	//	s.logger().Printf("[Server] serveConn:%d c:%d", connID, c)
	// 工作原理:
	// 针对一个c，循环读
	// * 读到内容，相应处理;然后循环中，再次阻塞在读
	// * 未读到，一直阻塞
	for {
		connRequestNum++
		ctx.time = currentTime
		//		ctx.Logger().Printf("connRequestNum:%d", connRequestNum)

		if s.ReadTimeout > 0 || s.MaxKeepaliveDuration > 0 {
			lastReadDeadlineTime = s.updateReadDeadline(c, ctx, lastReadDeadlineTime) // 更新读超时
			if lastReadDeadlineTime.IsZero() {
				err = ErrKeepaliveTimeout
				break
			}
		}

		// 读取器初始化：非‘最小内存模式’ 读取间隔<=1秒(在同连接上的后1请求)
		// 确认为该连接第2之后请求，使用首字节探测器-阻塞等待后续请求
		if !(s.ReduceMemoryUsage || ctx.lastReadDuration > time.Second) || br != nil {
			//			ctx.Logger().Printf("acquireReader")
			if br == nil {
				br = acquireReader(ctx)
			}
		} else { // 最小内存模式 || 读取间隔> 1秒 -- 使用首字节探测器-阻塞等待后续请求
			//			ctx.Logger().Printf("acquireByteReader")
			br, err = acquireByteReader(&ctx)
			//			ctx.Logger().Printf("acquireByteReader over c:%x", ctx.c)
		}
		ctx.Request.isTLS = isTLS

		//		ctx.Logger().Printf("start:%q", ctx.time.String())
		if err == nil { //阻塞-等待请求
			// 头处理
			if s.DisableHeaderNamesNormalizing {
				ctx.Request.Header.DisableNormalizing()
				ctx.Response.Header.DisableNormalizing()
			}
			// 读取body
			err = ctx.Request.readLimitBody(br, maxRequestBodySize, s.GetPostOnly)
			if br.Buffered() == 0 || err != nil { // 读取完成/出错(停止当前连接处理)
				//				ctx.Logger().Printf("releaseReader")
				releaseReader(s, br)
				br = nil
			}
		}

		currentTime = time.Now()
		ctx.lastReadDuration = currentTime.Sub(ctx.time) // 读取所花时间
		//		ctx.Logger().Printf("stop:%q", currentTime.String())

		if err != nil { // 申请读取器出错 || 首次读取出错
			if err == io.EOF { // 读取到末尾，请求结束
				err = nil
			} else { // 响应错误信息
				bw = writeErrorResponse(bw, ctx, err)
			}
			break
		}

		// 'Expect: 100-continue'请求处理
		// http://www.w3.org/Protocols/rfc2616/rfc2616-sec8.html
		if !ctx.Request.Header.noBody() && ctx.Request.MayContinue() {
			// 发送'HTTP/1.1 100 Continue'响应
			if bw == nil {
				bw = acquireWriter(ctx)
			}
			bw.Write(strResponseContinue)
			err = bw.Flush()
			releaseWriter(s, bw)
			bw = nil
			if err != nil { // 连接下发数据失败
				break
			}

			// 读取body
			if br == nil {
				br = acquireReader(ctx)
			}
			err = ctx.Request.ContinueReadBody(br, maxRequestBodySize)
			if br.Buffered() == 0 || err != nil { // 未读取到内容或出错
				releaseReader(s, br)
				br = nil
			}
			if err != nil {
				bw = writeErrorResponse(bw, ctx, err)
				break
			}
		}

		connectionClose = s.DisableKeepalive || ctx.Request.Header.connectionCloseFast()
		isHTTP11 = ctx.Request.Header.IsHTTP11()

		// 初始化ctx
		ctx.Response.Header.SetServerBytes(serverName)
		ctx.connID = connID
		ctx.connRequestNum = connRequestNum
		ctx.connTime = connTime
		ctx.time = currentTime
		s.Handler(ctx) // 调用用户设置的处理请求接口

		// 超时处理
		timeoutResponse = ctx.timeoutResponse
		if timeoutResponse != nil {
			ctx = s.acquireCtx(c) // todo?? leak ctx => gc
			timeoutResponse.CopyTo(&ctx.Response)
			if br != nil {
				// 因br有可能是与旧ctx.fbr关联，关闭连接
				ctx.SetConnectionClose()
			}
		}

		if !ctx.IsGet() && ctx.IsHead() {
			ctx.Response.SkipBody = true
		}
		ctx.Request.Reset() // 清理请求数据

		// 取定义的劫持处理接口
		hijackHandler = ctx.hijackHandler
		ctx.hijackHandler = nil

		ctx.userValues.Reset()

		// 该连接请求次数，超过最大值
		if s.MaxRequestsPerConn > 0 && connRequestNum >= uint64(s.MaxRequestsPerConn) {
			ctx.SetConnectionClose()
		}

		// 更新写超时
		if s.WriteTimeout > 0 || s.MaxKeepaliveDuration > 0 {
			lastWriteDeadlineTime = s.updateWriteDeadline(c, ctx, lastWriteDeadlineTime)
		}

		// 长连接处理
		// 因RequestHandler可能触发 header的解析，此处再次确认请求里的connectionClose
		connectionClose = connectionClose || ctx.Request.Header.connectionCloseFast() || ctx.Response.ConnectionClose()
		if connectionClose {
			ctx.Response.Header.SetCanonical(strConnection, strClose)
		} else if !isHTTP11 {
			// 'Connection: keep-alive'为非http/1.1，设置为长连接
			// HTTP/1.1默认长连接
			ctx.Response.Header.SetCanonical(strConnection, strKeepAlive)
		}

		// 重置响应里的serverName
		if len(ctx.Response.Header.Server()) == 0 {
			ctx.Response.Header.SetServerBytes(serverName)
		}

		if bw == nil {
			bw = acquireWriter(ctx)
		}
		// 响应数据失败-连接异常
		if err = writeResponse(ctx, bw); err != nil {
			break
		}

		// 读取器为空(读取完成/读取出错) 或 短连接
		if br == nil || connectionClose {
			//			ctx.Logger().Printf("bw.size:%d, %d, %d", bw.Size(), bw.Available(), bw.Buffered())
			err = bw.Flush()
			releaseWriter(s, bw)
			bw = nil
			if err != nil {
				break
			}
			if connectionClose { // 短连接
				break
			}
		}

		// 劫持处理-处理完退出
		if hijackHandler != nil { // todo??
			var hjr io.Reader
			hjr = c
			if br != nil {
				hjr = br
				br = nil

				// br有可能引用ctx.fbr,不能将ctx还给池 todo?? leak ctx => gc
				ctx = s.acquireCtx(c)
			}
			// 清空并释放bw
			if bw != nil {
				err = bw.Flush()
				releaseWriter(s, bw)
				bw = nil
				if err != nil {
					break
				}
			}
			c.SetReadDeadline(zeroTime)  // 劫持处理，不超时
			c.SetWriteDeadline(zeroTime) // 劫持处理，不超时
			go hijackConnHandler(hjr, c, s, hijackHandler)
			hijackHandler = nil
			err = errHijacked
			break
		}

		currentTime = time.Now()
	}

	if br != nil {
		releaseReader(s, br)
	}
	if bw != nil {
		releaseWriter(s, bw)
	}
	s.releaseCtx(ctx) // 释放ctx

	//	s.logger().Printf("[Server] serveConn over:%d c:%d", connID, c)
	return err
}

// 更新读超时时间
func (s *Server) updateReadDeadline(c net.Conn, ctx *RequestCtx, lastDeadlineTime time.Time) time.Time {
	readTimeout := s.ReadTimeout
	currentTime := ctx.time
	if s.MaxKeepaliveDuration > 0 {
		connTimeout := s.MaxKeepaliveDuration - currentTime.Sub(ctx.connTime)
		if connTimeout <= 0 { // 连接超时,通告上层接口
			return zeroTime
		}
		if connTimeout < readTimeout { // 读超时 <= 连接超时
			readTimeout = connTimeout
		}
	}

	// 最优处理: 当且仅当 写超时时间的25%(>>2 /4) 比 距离上次超时间隔小,更新写超时 todo??
	// See https://github.com/golang/go/issues/15133 for details.
	if currentTime.Sub(lastDeadlineTime) > (readTimeout >> 2) {
		if err := c.SetReadDeadline(currentTime.Add(readTimeout)); err != nil {
			panic(fmt.Sprintf("BUG: error in SetReadDeadline(%s): %s", readTimeout, err))
		}
		lastDeadlineTime = currentTime
	}
	return lastDeadlineTime
}

// 更新写超时时间 todo??
func (s *Server) updateWriteDeadline(c net.Conn, ctx *RequestCtx, lastDeadlineTime time.Time) time.Time {
	writeTimeout := s.WriteTimeout
	if s.MaxKeepaliveDuration > 0 { // 长连接的最大存活时间
		connTimeout := s.MaxKeepaliveDuration - time.Since(ctx.connTime)
		if connTimeout <= 0 {
			// 连接超时，用100ms，发送'Connection: close'头
			ctx.SetConnectionClose()
			connTimeout = 100 * time.Millisecond
		}
		if connTimeout < writeTimeout { // 写超时应<=连接超时
			writeTimeout = connTimeout
		}
	}

	// 最优处理: 当且仅当 写超时时间的25%(>>2 /4) 比 距离上次超时间隔小,更新写超时 todo??
	// See https://github.com/golang/go/issues/15133 for details.
	currentTime := time.Now()
	if currentTime.Sub(lastDeadlineTime) > (writeTimeout >> 2) {
		if err := c.SetWriteDeadline(currentTime.Add(writeTimeout)); err != nil {
			panic(fmt.Sprintf("BUG: error in SetWriteDeadline(%s): %s", writeTimeout, err))
		}
		lastDeadlineTime = currentTime
	}
	return lastDeadlineTime
}

// --- hijackConn pool
// serveConn调用
func hijackConnHandler(r io.Reader, c net.Conn, s *Server, h HijackHandler) {
	hjc := s.acquireHijackConn(r, c)
	h(hjc)

	if br, ok := r.(*bufio.Reader); ok {
		releaseReader(s, br)
	}
	c.Close()
	s.releaseHijackConn(hjc)
}
func (s *Server) acquireHijackConn(r io.Reader, c net.Conn) *hijackConn {
	v := s.hijackConnPool.Get()
	if v == nil {
		hjc := &hijackConn{
			Conn: c,
			r:    r,
		}
		return hjc
	}
	hjc := v.(*hijackConn)
	hjc.Conn = c
	hjc.r = r
	return hjc
}
func (s *Server) releaseHijackConn(hjc *hijackConn) {
	hjc.Conn = nil
	hjc.r = nil
	s.hijackConnPool.Put(hjc)
}

// --- hijackConn
type hijackConn struct {
	net.Conn
	r io.Reader
}

func (c hijackConn) Read(p []byte) (int, error) {
	return c.r.Read(p)
}

// hijackConnHandler触发关闭连接
func (c hijackConn) Close() error {
	// hijacked conn is closed in hijackConnHandler
	return nil
}

// 返回TimeoutError*设置的超时响应
// 用于用户自定义接口
func (ctx *RequestCtx) LastTimeoutErrorResponse() *Response {
	return ctx.timeoutResponse
}

func writeResponse(ctx *RequestCtx, w *bufio.Writer) error {
	if ctx.timeoutResponse != nil { // todo??
		panic("BUG: cannot write timed out response")
	}
	err := ctx.Response.Write(w)
	ctx.Response.Reset()
	return err
}

const (
	defaultReadBufferSize  = 1024 //原生go的http，也是这个值，看需求，360服务器改为1k todo??
	defaultWriteBufferSize = 1024
)

// 首字节探测器
func acquireByteReader(ctxP **RequestCtx) (*bufio.Reader, error) {
	ctx := *ctxP
	s := ctx.s
	c := ctx.c
	t := ctx.time
	s.releaseCtx(ctx) // 仅需置空c

	// 让gc回收资源
	ctx = nil
	*ctxP = nil

	v := s.bytePool.Get()
	if v == nil {
		v = make([]byte, 1)
	}
	b := v.([]byte)
	n, err := c.Read(b) // 监听首字节
	ch := b[0]
	s.bytePool.Put(v)
	ctx = s.acquireCtx(c)

	ctx.time = t
	*ctxP = ctx
	if err != nil {
		// 首字节读取失败，都当作io.EOF错误处理
		return nil, io.EOF
	}
	if n != 1 {
		panic("BUG: Reader must return at least one byte")
	}

	ctx.fbr.c = c
	ctx.fbr.ch = ch          // 首字节
	ctx.fbr.byteRead = false // 还未把首字节使用掉
	r := acquireReader(ctx)
	r.Reset(&ctx.fbr)
	return r, nil
}

// --- Reader pool 读缓冲器
func acquireReader(ctx *RequestCtx) *bufio.Reader {
	v := ctx.s.readerPool.Get()
	if v == nil {
		n := ctx.s.ReadBufferSize
		if n <= 0 {
			n = defaultReadBufferSize
		}
		return bufio.NewReaderSize(ctx.c, n)
	}
	r := v.(*bufio.Reader)
	r.Reset(ctx.c)
	return r
}
func releaseReader(s *Server, r *bufio.Reader) {
	r.Reset(nil) // +优化
	s.readerPool.Put(r)
}

// --- Writer pool 写缓冲器
func acquireWriter(ctx *RequestCtx) *bufio.Writer {
	//	ctx.Logger().Printf("acquireWriter:%x", ctx.c)
	v := ctx.s.writerPool.Get()
	if v == nil {
		//		ctx.Logger().Printf("acquireWriter: v== nil")
		n := ctx.s.WriteBufferSize
		if n <= 0 {
			n = defaultWriteBufferSize
		}
		return bufio.NewWriterSize(ctx.c, n)
	}
	w := v.(*bufio.Writer)
	w.Reset(ctx.c)
	//	ctx.Logger().Printf("acquireWriter:%x, %+v, %x", ctx.c, w, GetAddr(w))
	return w
}

func releaseWriter(s *Server, w *bufio.Writer) {
	//	s.logger().Printf("releaseWriter:%+v, %x", w, GetAddr(w))
	w.Reset(nil) // +优化
	s.writerPool.Put(w)
}

func (s *Server) acquireCtx(c net.Conn) *RequestCtx {
	v := s.ctxPool.Get()
	var ctx *RequestCtx
	if v == nil {
		ctx = &RequestCtx{
			s: s,
		}
		keepBodyBuffer := !s.ReduceMemoryUsage
		ctx.Request.keepBodyBuffer = keepBodyBuffer
		ctx.Response.keepBodyBuffer = keepBodyBuffer
	} else {
		ctx = v.(*RequestCtx)
		// +优化 todo??
		ctx.Reset()
		ctx.s = s
		keepBodyBuffer := !s.ReduceMemoryUsage
		ctx.Request.keepBodyBuffer = keepBodyBuffer
		ctx.Response.keepBodyBuffer = keepBodyBuffer
	}
	ctx.c = c
	return ctx
}

// 初始化ctx,用于传到RequestHandler
// remoteAddr和logger可选，用于RequestCtx.logger
// 该函数用于自定Server接口
// See https://github.com/valyala/httpteleport for details. todo??
func (ctx *RequestCtx) Init2(conn net.Conn, logger Logger, reduceMemoryUsage bool) {
	ctx.c = conn
	ctx.logger.logger = logger
	ctx.connID = nextConnID()
	ctx.s = fakeServer
	ctx.connRequestNum = 0
	ctx.connTime = time.Now()
	ctx.time = ctx.connTime

	keepBodyBuffer := !reduceMemoryUsage
	ctx.Request.keepBodyBuffer = keepBodyBuffer
	ctx.Response.keepBodyBuffer = keepBodyBuffer
}

// 初始化ctx,用于传到RequestHandler
// remoteAddr和logger可选，用于RequestCtx.logger
// 该函数用于自定Server接口
func (ctx *RequestCtx) Init(req *Request, remoteAddr net.Addr, logger Logger) {
	if remoteAddr == nil {
		remoteAddr = zeroTCPAddr
	}
	c := &fakeAddrer{
		laddr: zeroTCPAddr,
		raddr: remoteAddr,
	}
	if logger == nil {
		logger = defaultLogger
	}
	ctx.Init2(c, logger, true)
	req.CopyTo(&ctx.Request)
}

// todo??测试用
var fakeServer = &Server{
	concurrencyCh: make(chan struct{}, DefaultConcurrency),
}

// --- fakeAddrer todo??测试用
type fakeAddrer struct {
	net.Conn
	laddr net.Addr
	raddr net.Addr
}

func (fa *fakeAddrer) RemoteAddr() net.Addr {
	return fa.raddr
}
func (fa *fakeAddrer) LocalAddr() net.Addr {
	return fa.laddr
}
func (fa *fakeAddrer) Read(p []byte) (int, error) {
	panic("BUG: unexpected Read call")
}
func (fa *fakeAddrer) Write(p []byte) (int, error) {
	panic("BUG: unexpected Write call")
}
func (fa *fakeAddrer) Close() error {
	panic("BUG: unexpected Close call")
}

func (s *Server) releaseCtx(ctx *RequestCtx) {
	if ctx.timeoutResponse != nil {
		panic("BUG: cannot release timed out RequestCtx")
	}
	ctx.c = nil
	ctx.fbr.c = nil
	s.ctxPool.Put(ctx)
}

//取服务器名
func (s *Server) getServerName() []byte {
	v := s.serverName.Load()
	var serverName []byte
	if v == nil {
		serverName = []byte(s.Name)
		if len(serverName) == 0 {
			serverName = defaultServerName
		}
		s.serverName.Store(serverName)
	} else {
		serverName = v.([]byte)
	}
	return serverName
}

func (s *Server) writeFastError(w io.Writer, statusCode int, msg string) {
	w.Write(statusLine(statusCode))
	fmt.Fprintf(w, "Connection: close\r\n"+
		"Server: %s\r\n"+
		"Date: %s\r\n"+
		"Content-Type: text/plain\r\n"+
		"Content-Length: %d\r\n"+
		"\r\n"+
		"%s",
		s.getServerName(), serverDate.Load(), len(msg), msg)
}

//  todo??
// 将错误写入响应，给客户端
func writeErrorResponse(bw *bufio.Writer, ctx *RequestCtx, err error) *bufio.Writer {
	if _, ok := err.(*ErrSmallBuffer); ok {
		ctx.Error("Too big request header", StatusRequestHeaderFieldsTooLarge)
	} else {
		ctx.Error("Error when parsing request", StatusBadRequest)
	}
	ctx.SetConnectionClose()
	if bw == nil {
		bw = acquireWriter(ctx)
	}
	writeResponse(ctx, bw)
	bw.Flush()
	return bw
}
