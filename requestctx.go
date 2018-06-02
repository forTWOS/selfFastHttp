package selfFastHttp

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"os"
	"sync"
	"time"
)

//===================================
// RequestCtx 包含请求，并管理响应
// 禁止拷贝
// RequestHandler应避免在返回后，继续使用RequestCtx内成员
// 如果在返回后，确实要用引用RequestCtx内成员，则RequestHandler在返回前，必须调用ctx.TimeoutError()
//
// 多协程并发读或改RequestCtx是不安全的，TimeoutError*是惟一可并发时使用
type RequestCtx struct {
	noCopy noCopy

	// 到来的请求
	// 禁止值拷贝，使用指针
	Request Request

	// 下发的响应内容
	// 禁止值拷贝，使用指针
	Response Response

	//玩家数据-键值对
	userValues userData

	//最后一次读操作所花时间
	lastReadDuration time.Duration

	connID         uint64    //连接ID
	connRequestNum uint64    //该连接的请求次数-受Server.MaxRequestsPerConn限制
	connTime       time.Time //连接建立时间-受Server.MaxKeepaliveDuration限制

	time time.Time // 该连接的RequestHandle被调用时间 或读操作开始时间-计算最后一次读操作所花时间

	logger ctxLogger //日志imp
	s      *Server
	c      net.Conn
	fbr    firstByteReader //特定条件使用的读取器

	timeoutResponse *Response     //超时标志器-用于超时后相关处理
	timeoutCh       chan struct{} //超时管道,在等待处理结束时，定时
	timeoutTimer    *time.Timer   //超时定时器，与timeoutCh合用，最后调用TimeoutError设置timeoutResponse

	hijackHandler HijackHandler
}

// todo??
func (ctx *RequestCtx) Reset() { // +优化
	ctx.Request.Reset()
	ctx.Response.Reset()
	ctx.userValues.Reset()

	ctx.lastReadDuration = 0
	ctx.connID = 0
	ctx.connRequestNum = 0
	ctx.connTime = zeroTime

	ctx.time = zeroTime
	ctx.logger.Reset()
	ctx.s = nil
	ctx.c = nil
	ctx.fbr.c = nil

	ctx.timeoutResponse = nil
	ctx.timeoutCh = nil
	ctx.timeoutTimer = nil

	ctx.hijackHandler = nil
}

//===================================
// 劫持处理接口,在正常Server请求接口处理完成后，执行该接口
type HijackHandler func(c net.Conn)

//-----------------------------------
// 注册劫持接口
// 触发时机: RequestHandler执行完，response发送前
// 劫持处理完，连接将自动关闭
// 不触发：
//    当'Connection: close'头在request 或response已存在
//    发送响应内容时出错
// Server停止处理hijack连接
// Server限制(最大并发数、读超时时间、写超时时间)将不生效
//
// 该接口须不引用ctx成员
// 任意的'Connection: Upgrade' 协议可能应用该接口,.e.g:
//   * WebSocket
//   * HTTP/2.0
//
func (ctx *RequestCtx) Hijack(handler HijackHandler) {
	ctx.hijackHandler = handler
}

// 检测劫持接口是否已设置
func (ctx *RequestCtx) Hijacked() bool {
	return ctx.hijackHandler != nil
}

// 控key存储任意value
// value可能包含UserValue*-嵌套
// 请求流程中，函数间传任意值
// 当从RequestHandler返回，所有值被移除;另外，所有value会尝试io.Closer关闭操作
func (ctx *RequestCtx) SetUserValue(key string, value interface{}) {
	ctx.userValues.Set(key, value)
}

// 控key存储任意value
// value可能包含UserValue*-嵌套
// 请求流程中，函数间传任意值
// 当从RequestHandler返回，所有值被移除;另外，所有value会尝试io.Closer关闭操作
func (ctx *RequestCtx) SetUserValueBytes(key []byte, value interface{}) {
	ctx.userValues.SetBytes(key, value)
}

func (ctx *RequestCtx) UserValue(key string) interface{} {
	return ctx.userValues.Get(key)
}
func (ctx *RequestCtx) UserValueBytes(key []byte) interface{} {
	return ctx.userValues.GetBytes(key)
}

func (ctx *RequestCtx) VisitUserValues(visitor func([]byte, interface{})) {
	for i, n := 0, len(ctx.userValues); i < n; i++ {
		kv := &ctx.userValues[i]
		visitor(kv.key, kv.value)
	}
}

// --- connTlsSer
type connTLSer interface {
	ConnectionState() tls.ConnectionState
}

// tls.Conn is an encrypted connection (aka SSL, HTTPS).
func (ctx *RequestCtx) IsTLS() bool {
	// 转换成connTLSer，而不是*tls.Conn，是因为有此情况，会覆盖tls.Conn,例如: todo??
	//
	// type customConn struct {
	//	    *tls.Conn
	//
	//		// other custom fields
	// }
	_, ok := ctx.c.(connTLSer)
	return ok
}

// TLS connection state.
// 非tls，返回nil
// 返回值，可用于确定tls版本,客户端证书
func (ctx *RequestCtx) TLSConnectionState() *tls.ConnectionState {
	tlsConn, ok := ctx.c.(connTLSer)
	if !ok {
		return nil
	}
	state := tlsConn.ConnectionState()
	return &state
}

//===================================
// 首字节读取器
// 条件:Server.ReduceMemoryUsage开启 或 最后一次读操作时间超过1秒
// 目的:初始读缓存时，先读取第1字节;降低ctx使用??
type firstByteReader struct {
	c        net.Conn
	ch       byte
	byteRead bool
}

// 在net.Conn上封装了Read接口
func (r *firstByteReader) Read(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	nn := 0
	if !r.byteRead {
		b[0] = r.ch
		b = b[1:]
		r.byteRead = true
		nn = 1
	}
	n, err := r.c.Read(b)
	return n + nn, err
}

type Logger interface {
	Printf(format string, args ...interface{})
}

//===================================
// 请求的日志输出，用一个全局锁 todo
var ctxLoggerLock sync.Mutex

//===================================
// 目的：写日志时，自动输出RequestCtx部分信息
type ctxLogger struct {
	ctx    *RequestCtx
	logger Logger
}

func (cl *ctxLogger) Printf(format string, args ...interface{}) {
	ctxLoggerLock.Lock()
	msg := fmt.Sprintf(format, args...)
	ctx := cl.ctx
	cl.logger.Printf("%.3f %s - %s", time.Since(ctx.Time()).Seconds(), ctx.String(), msg)
	ctxLoggerLock.Unlock()
}
func (cl *ctxLogger) Reset() {
	cl.ctx = nil
	cl.logger = nil
}

//===================================
var zeroTCPAddr = &net.TCPAddr{
	IP: net.IPv4zero,
}

//---------------------------------
func (ctx *RequestCtx) String() string {
	return fmt.Sprintf("#%016X - %s<->%s - %s %s", ctx.ID(), ctx.LocalAddr(), ctx.RemoteAddr(), ctx.Request.Header.Method(), ctx.URI().FullURI())
}

//返回惟一的请求id
func (ctx *RequestCtx) ID() uint64 {
	return (ctx.connID << 32) | ctx.connRequestNum
}
func (ctx *RequestCtx) ConnID() uint64 {
	return ctx.connID
}
func (ctx *RequestCtx) Time() time.Time {
	return ctx.time
}

func (ctx *RequestCtx) ConnTime() time.Time {
	return ctx.connTime
}

// 返回当前连接的请求序号，从1开始
func (ctx *RequestCtx) ConnRequestNum() uint64 {
	return ctx.connRequestNum
}

// 响应头:'Connection: close'
// 当RequestHandler处理完闭，关闭连接
func (ctx *RequestCtx) SetConnectionClose() {
	ctx.Response.SetConnectionClose()
}

func (ctx *RequestCtx) SetStatusCode(statusCode int) {
	ctx.Response.SetStatusCode(statusCode)
}

// 设置:响应类型
func (ctx *RequestCtx) SetContentType(contentType string) {
	ctx.Response.Header.SetContentType(contentType)
}

// 复制contentType，函数返回后，可直接处理之
func (ctx *RequestCtx) SetContentTypeBytes(contentType []byte) {
	ctx.Response.Header.SetContentTypeBytes(contentType)
}

// todo?? i.e. 仅URI部份，不含scheme,host
func (ctx *RequestCtx) RequestURI() []byte {
	return ctx.Request.Header.RequestURI()
}

// request uri模块
func (ctx *RequestCtx) URI() *URI {
	return ctx.Request.URI()
}

// 头响中referer内容
func (ctx *RequestCtx) Referer() []byte {
	return ctx.Request.Header.Referer()
}

// 请求中的用户代理
func (ctx *RequestCtx) UserAgent() []byte {
	return ctx.Request.Header.UserAgent()
}

func (ctx *RequestCtx) Path() []byte {
	return ctx.URI().Path()
}

func (ctx *RequestCtx) Host() []byte {
	return ctx.URI().Host()
}

func (ctx *RequestCtx) QueryArgs() *Args {
	return ctx.URI().QueryArgs()
}

func (ctx *RequestCtx) PostArgs() *Args {
	return ctx.Request.PostArgs()
}

// requests's multipart form.
// 若不包启'multipart/form-data'头，返回ErrNoMultipartForm
// 所有上传的临时文件，在RequestHandler返回后，删除;直接移动或复制文件到新地方，来保存之
// SaveMultipartFile用于永久保存上传的文件
func (ctx *RequestCtx) MultipartForm() (*multipart.Form, error) {
	return ctx.Request.MultipartForm()
}

// 按key返回上传的文件
// 所有上传的临时文件，在RequestHandler返回后，删除;直接移动或复制文件到新地方，来保存之
// SaveMultipartFile用于永久保存上传的文件
func (ctx *RequestCtx) FormFile(key string) (*multipart.FileHeader, error) {
	mf, err := ctx.MultipartForm()
	if err != nil {
		return nil, err
	}
	if mf.File == nil {
		return nil, err
	}
	fhh := mf.File[key]
	if fhh == nil {
		return nil, ErrMissingFile
	}
	return fhh[0], nil
}

var ErrMissingFile = errors.New("there is no uploaded file associated with the given key")

// 永久保存上传文件 成 新指定文件
func SaveMultipartFile(fh *multipart.FileHeader, path string) error {
	f, err := fh.Open()
	if err != nil {
		return err
	}
	defer f.Close()

	if ff, ok := f.(*os.File); ok { // 直实文件，直接重命名并移到指定路径
		return os.Rename(ff.Name(), path)
	}

	ff, err := os.Create(path)
	if err != nil {
		return err
	}
	defer ff.Close()
	_, err = copyZeroAlloc(ff, f)
	return err
}

// 按key，在Query、post、put中查找值
// 包含该值的细化方法:
// * QueryArgs - query string
// * PostArgs - POST/PUT body
// * MultipartForm - multipart form
// * FormFile - uploaded file
func (ctx *RequestCtx) FormValue(key string) []byte {
	v := ctx.QueryArgs().Peek(key)
	if len(v) > 0 {
		return v
	}
	v = ctx.PostArgs().Peek(key)
	if len(v) > 0 {
		return v
	}
	mf, err := ctx.MultipartForm()
	if err == nil && mf.Value != nil {
		vv := mf.Value[key] // map[string][]string
		if len(vv) > 0 {
			return []byte(vv[0])
		}
	}
	return nil
}

func (ctx *RequestCtx) IsGet() bool {
	return ctx.Request.Header.IsGet()
}
func (ctx *RequestCtx) IsPost() bool {
	return ctx.Request.Header.IsPost()
}
func (ctx *RequestCtx) IsPut() bool {
	return ctx.Request.Header.IsPut()
}
func (ctx *RequestCtx) IsDelete() bool {
	return ctx.Request.Header.IsDelete()
}

func (ctx *RequestCtx) Method() []byte {
	return ctx.Request.Header.Method()
}

func (ctx *RequestCtx) IsHead() bool {
	return ctx.Request.Header.IsHead()
}

func (ctx *RequestCtx) RemoteAddr() net.Addr {
	if ctx.c == nil {
		return zeroTCPAddr
	}
	addr := ctx.c.RemoteAddr()
	if addr == nil {
		return zeroTCPAddr
	}
	return addr
}

// 保证返回non-nil值
func (ctx *RequestCtx) LocalAddr() net.Addr {
	if ctx.c == nil {
		return zeroTCPAddr
	}
	addr := ctx.c.LocalAddr()
	if addr == nil {
		return zeroTCPAddr
	}
	return addr
}

// 返回远程网络地址
func (ctx *RequestCtx) RemoteIP() net.IP {
	return addrToIP(ctx.RemoteAddr())
}

func (ctx *RequestCtx) LocalIP() net.IP {
	return addrToIP(ctx.LocalAddr())
}
func addrToIP(addr net.Addr) net.IP {
	x, ok := addr.(*net.TCPAddr)
	if !ok {
		return net.IPv4zero
	}
	return x.IP
}

// Error设置响应错误码、信息
func (ctx *RequestCtx) Error(msg string, statusCode int) {
	ctx.Response.Reset()
	ctx.SetStatusCode(statusCode)
	ctx.SetContentTypeBytes(defaultContentType)
	ctx.SetBodyString(msg)
}

func (ctx *RequestCtx) Success(contentType string, body []byte) {
	ctx.SetContentType(contentType)
	ctx.SetBody(body)
}

func (ctx *RequestCtx) SuccessString(contentType, body string) {
	ctx.SetContentType(contentType)
	ctx.SetBodyString(body)
}

// Redirect 返回'Location: uri'头和状态码
// 状态码:
//    301 被请求的资源已永久移动到新位置
//    302 请求的资源现在临时从不同的 URI 响应请求
//    303 对应当前请求的响应可以在另一个 URI 上被找到，而且客户端应当采用 GET 的方式访问那个资源。
//    307 请求的资源现在临时从不同的URI 响应请求。
//  其它状态码，将转为302
// 跳转uri有可能与现uri是相对或绝对关系
func (ctx *RequestCtx) Redirect(uri string, statusCode int) {
	u := AcquireURI()
	ctx.URI().CopyTo(u)
	u.Update(uri)
	ctx.redirect(u.FullURI(), statusCode)
	ReleaseURI(u)
}

// RedirectBytes 返回'Location: uri'头和状态码
// 状态码:
//    301 被请求的资源已永久移动到新位置
//    302 请求的资源现在临时从不同的 URI 响应请求
//    303 对应当前请求的响应可以在另一个 URI 上被找到，而且客户端应当采用 GET 的方式访问那个资源。
//    307 请求的资源现在临时从不同的URI 响应请求。
//  其它状态码，将转为302
// 跳转uri有可能与现uri是相对或绝对关系
func (ctx *RequestCtx) RedirectBytes(uri []byte, statusCode int) {
	s := b2s(uri)
	ctx.Redirect(s, statusCode)
}

// 重定向
// 1.设置重定向头
// 2.获取重定向码
// 3.响应重定向码
func (ctx *RequestCtx) redirect(uri []byte, statusCode int) {
	ctx.Response.Header.SetCanonical(strLocation, uri)
	statusCode = getRedirectStatusCode(statusCode)
	ctx.Response.SetStatusCode(statusCode)
}

// 301,302,303,307
func getRedirectStatusCode(statusCode int) int {
	if statusCode == StatusMovedPermanently || statusCode == StatusFound ||
		statusCode == StatusSeeOther || statusCode == StatusTemporaryRedirect {
		return statusCode
	}
	return StatusFound
}

// 设置响应body
func (ctx *RequestCtx) SetBody(body []byte) {
	ctx.Response.SetBody(body)
}
func (ctx *RequestCtx) SetBodyString(body string) {
	ctx.Response.SetBodyString(body)
}

// 重置响应Body
func (ctx *RequestCtx) ResetBody() {
	ctx.Response.ResetBody()
}

// 将路径所指的本地文件，当作响应内容
//   是ServeFile(ctx, path)的快捷方式
// 参考: ServeFile, FSHandler, FS
func (ctx *RequestCtx) SendFile(path string) {
	ServeFile(ctx, path)
}

// 将路径所指本地文件，当作响应内容
//   是ServeFileBytes(ctx, path)的快捷方式
// 参考: ServeFileBytes, FSHandler, FS
func (ctx *RequestCtx) SendFileBytes(path []byte) {
	ServeFileBytes(ctx, path)
}

// 检测客户端缓存文件，与服务端该文件的最后修改时间，相应处理
//   If-Modified-Since是标准的HTTP请求头标签，在发送HTTP请求时，把浏览器端缓存页面的最后修改时间一起发到服务器去，服务器会把这个时间与服务器上实际文件的最后修改时间进行比较。
//   如果时间一致，那么返回HTTP状态码304（不返回文件内容），客户端接到之后，就直接把本地缓存文件显示到浏览器中。
//   如果时间不一致，就返回HTTP状态码200和新的文件内容，客户端接到之后，会丢弃旧文件，把新文件缓存起来，并显示到浏览器中。
func (ctx *RequestCtx) IfModifiedSince(lastModified time.Time) bool {
	ifModStr := ctx.Request.Header.peek(strIfModifiedSince)
	if len(ifModStr) == 0 {
		return true
	}
	ifMod, err := ParseHTTPDate(ifModStr)
	if err != nil {
		return true
	}
	lastModified = lastModified.Truncate(time.Second)
	return ifMod.Before(lastModified)
}

// 配合IfModifiedSince(),发送未改变响应
func (ctx *RequestCtx) NotModified() {
	ctx.Response.Reset()
	ctx.SetStatusCode(StatusNotModified)
}

// 404响应
func (ctx *RequestCtx) NotFound() {
	ctx.Response.Reset()
	ctx.SetStatusCode(StatusNotFound)
	ctx.SetBodyString("404 Page not found")
}

func (ctx *RequestCtx) Write(p []byte) (int, error) {
	ctx.Response.AppendBody(p)
	return len(p), nil
}

func (ctx *RequestCtx) WriteString(s string) (int, error) {
	ctx.Response.AppendBodyString(s)
	return len(s), nil
}

// 在RequestHandler返回前有效
func (ctx *RequestCtx) PostBody() []byte {
	return ctx.Request.Body()
}

// 触发关闭:Body,BodyWriteTo,AppendBodyString,SetBodyStream,ResetBody,ReleaseBody,SwapBody,writeBodyStream
// * bodySize>=0，则bodyStream需提供相应字节
// * bodySize<0，则读取直到io.EOF
// ps: GET,HEAD没有body
func (ctx *RequestCtx) SetBodyStream(bodyStream io.Reader, bodySize int) {
	ctx.Response.SetBodyStream(bodyStream, bodySize)
}

// 情景:
// * body太大，超过10M
// * body是从外部慢源取流数据
// * body需要分片的 - `http client push` `chunked transfer-encoding`
func (ctx *RequestCtx) SetBodyStreamWriter(sw StreamWriter) {
	ctx.Response.SetBodyStreamWriter(sw)
}

func (ctx *RequestCtx) IsBodyStream() bool {
	return ctx.Response.IsBodyStream()
}

// logger用于在RequestHandler记录 请求的任意信息
// 所有记录的信息包含:request id, request duration, local address, remote address,
// request method and request url
// 可重复利用返回的logger来记录当次请求信息
// 在RequestHandler返回前有效
func (ctx *RequestCtx) Logger() Logger {
	if ctx.logger.ctx == nil {
		ctx.logger.ctx = ctx
	}
	if ctx.logger.logger == nil {
		ctx.logger.logger = ctx.s.logger() //ctx.Server.Logger
	}
	return &ctx.logger
}

// 设置超时响应码和响应内容
// 所有在该方法之后的响应变更，都被忽略 todo??
// 若在其它协程中，有涉及到ctx的成员，TimeoutError须在RequestHandler返回前调用
// 不鼓励使用该方法,最好直接在挂起的协程中，消除对ctx的引用 todo??
func (ctx *RequestCtx) TimeoutError(msg string) {
	ctx.TimeoutErrorWithCode(msg, StatusRequestTimeout)
}

// 设置超时响应码和响应内容
// 所有在该方法之后的响应变更，都被忽略
// 若在其它协程中，有涉及到ctx的成员，TimeoutErrorWithCode须在RequestHandler返回前调用
// 不鼓励使用该方法,最好直接在挂起的协程中，消除对ctx的引用
func (ctx *RequestCtx) TimeoutErrorWithCode(msg string, statusCode int) {
	var resp Response
	resp.SetStatusCode(statusCode)
	resp.SetBodyString(msg)
	ctx.TimeoutErrorWithResponse(&resp)
}

// 将ctx标记为超时,发送给定的响应
// 所有在该方法之后的响应变更，都被忽略
// 若在其它协程中，有涉及到ctx的成员，TimeoutErrorWithResponse须在RequestHandler返回前调用
// 不鼓励使用该方法,最好直接在挂起的协程中，消除对ctx的引用
func (ctx *RequestCtx) TimeoutErrorWithResponse(resp *Response) {
	respCopy := &Response{}
	resp.CopyTo(respCopy)
	ctx.timeoutResponse = respCopy
}
