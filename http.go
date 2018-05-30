package selfFastHttp

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"sync"

	"github.com/valyala/bytebufferpool"
)

// 禁止直接复制值,正确做法:新建并CopyTo
// 不可用于并发
type Request struct {
	noCopy noCopy

	// 禁止直接复制值,正确做法:新建并CopyTo
	Header RequestHeader

	// 请求信息: query+post
	uri      URI
	postArgs Args // application/x-www-form-urlencoded

	bodyStream io.Reader
	w          requestBodyWriter
	body       *bytebufferpool.ByteBuffer //todo

	// 复合form:multipart/form-data
	multipartForm         *multipart.Form
	multipartFormBoundary string

	// 聚集bool,减少Request对象大小:sizeof(go.bool)=1bit
	parsedURI      bool // 是否已解析过URI
	parsedPostArgs bool // 是否已解析过post:application/x-www-form-urlencoded

	// 请求结束时，是否保留BodyBuffer
	// server: 是否开启省内存模式
	// client: 默认关闭, 重定向时:开启
	keepBodyBuffer bool

	isTLS bool
}

// 禁止直接复制值,正确做法:新建并CopyTo
// 不可用于并发
type Response struct {
	noCopy noCopy

	// 禁止直接复制值,正确做法:新建并CopyTo
	Header ResponseHeader

	bodyStream io.Reader
	w          responseBodyWriter
	body       *bytebufferpool.ByteBuffer

	// 跳过 读取/写入 Body内容
	// 在响head方法中使用
	SkipBody bool

	// 请求结束时，是否保留BodyBuffer
	// server: 是否开启省内存模式
	// client: 默认关闭, 重定向时:开启
	keepBodyBuffer bool
}

// =================================
// --- Req.Host
func (req *Request) SetHost(host string) {
	req.URI().SetHost(host)
}
func (req *Request) SetHostBytes(host []byte) {
	req.URI().SetHostBytes(host)
}
func (req *Request) Host() []byte {
	return req.URI().Host()
}

// --- Req.RequestURI
func (req *Request) SetRequestURI(requestURI string) {
	req.Header.SetRequestURI(requestURI)
	req.parsedURI = false
}
func (req *Request) SetRequestURIBytes(requestURI []byte) {
	req.Header.SetRequestURIBytes(requestURI)
	req.parsedURI = false
}
func (req *Request) RequestURI() []byte {
	if req.parsedURI { // 因parsedURI仅直接设置Header中RequestURI时，才false;在true状态下，有可能uri信息是最新的;在uri中做开关，而无需每次组装uri
		requestURI := req.uri.RequestURI()
		req.SetRequestURIBytes(requestURI)
	}
	return req.Header.RequestURI()
}

// --- Resp.StatusCode
func (resp *Response) StatusCode() int {
	return resp.Header.StatusCode()
}
func (resp *Response) SetStatusCode(statusCode int) {
	resp.Header.SetStatusCode(statusCode)
}

// --- ConnectionClose
func (resp *Response) ConnectionClose() bool {
	return resp.Header.ConnectionClose()
}
func (resp *Response) SetConnectionClose() {
	resp.Header.SetConnectionClose()
}
func (req *Request) ConnectionClose() bool {
	return req.Header.ConnectionClose()
}
func (req *Request) SetConnectionClose() {
	req.Header.SetConnectionClose()
}

// --- Resp.SendFile
// 将本地文件内容，作为响应内容
// ps:该接口未设置Content-Type,需另外主动设置
// 1.打开文件 - 关闭文件
// 2.查看文件大小：是否超过int最大值
// 3.文件修改时间:作为头部-修改时间
//
func (resp *Response) SendFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	fileInfo, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	size64 := fileInfo.Size()
	size := int(size64)
	if int64(size) != size64 {
		size = -1
	}

	resp.Header.SetLastModified(fileInfo.ModTime())
	resp.SetBodyStream(f, size)
	return nil
}

// --- SetBodyStream
// 触发关闭:Body,BodyWriteTo,AppendBodyString,SetBodyStream,ResetBody,ReleaseBody,SwapBody,writeBodyStream
// * bodySize>=0，则bodyStream需提供相应字节
// * bodySize<0，则读取直到io.EOF
// ps: GET,HEAD没有body
func (req *Request) SetBodyStream(bodyStream io.Reader, bodySize int) {
	req.ResetBody()
	req.bodyStream = bodyStream
	req.Header.SetContentLength(bodySize)
}
func (resp *Response) SetBodyStream(bodyStream io.Reader, bodySize int) {
	resp.ResetBody()
	resp.bodyStream = bodyStream
	resp.Header.SetContentLength(bodySize)
}

// --- IsBodyStream
// 确认body是通过bodyStream获取的源数据
func (req *Request) IsBodyStream() bool {
	return req.bodyStream != nil
}
func (resp *Response) IsBodyStream() bool {
	return resp.bodyStream != nil
}

// --- SetBodyStreamWriter
// 情景:
// * body太大，超过10M
// * body是从外部慢源取流数据
// * body需要分片的 - `http client push` `chunked transfer-encoding`
func (req *Request) SetBodyStreamWriter(sw StreamWriter) {
	sr := NewStreamReader(sw) // 将源数据，通过协程，后台读取数据到管道
	req.SetBodyStream(sr, -1)
}
func (resp *Response) SetBodyStreamWriter(sw StreamWriter) {
	sr := NewStreamReader(sw) // 将源数据，通过协程，后台读取数据到管道
	resp.SetBodyStream(sr, -1)
}

// --- BodyWriter
// 用于RequestHandler内部,在其结束后，不可使用
// RequestCtx.Write or SetBodyStreamWriter
func (resp *Response) BodyWriter() io.Writer {
	resp.w.r = resp
	return &resp.w
}
func (req *Request) BodyWriter() io.Writer {
	req.w.r = req
	return &req.w
}

// ====================================
// --- responseBodyWriter
type responseBodyWriter struct {
	r *Response
}

// 将p内容附到body上
func (w *responseBodyWriter) Write(p []byte) (int, error) {
	w.r.AppendBody(p)
	return len(p), nil
}

// --- requestBodyWriter
type requestBodyWriter struct {
	r *Request
}

func (w *requestBodyWriter) Write(p []byte) (int, error) {
	w.r.AppendBody(p)
	return len(p), nil
}

// ================================
// --- Resp.Body
func (resp *Response) Body() []byte {
	if resp.bodyStream != nil {
		bodyBuf := resp.bodyBuffer()
		bodyBuf.Reset()
		_, err := copyZeroAlloc(bodyBuf, resp.bodyStream)
		resp.closeBodyStream()
		if err != nil {
			bodyBuf.SetString(err.Error())
		}
	}
	return resp.bodyBytes()
}

// --- bodyBytes
func (resp *Response) bodyBytes() []byte {
	if resp.body == nil {
		return nil
	}
	return resp.body.B
}
func (req *Request) bodyBytes() []byte {
	if req.body == nil {
		return nil
	}
	return req.body.B
}

// --- bodyBuffer
func (resp *Response) bodyBuffer() *bytebufferpool.ByteBuffer {
	if resp.body == nil {
		resp.body = responseBodyPool.Get()
		resp.body.Reset() // +优化
	}
	return resp.body
}
func (req *Request) bodyBuffer() *bytebufferpool.ByteBuffer {
	if req.body == nil {
		req.body = requestBodyPool.Get()
		req.body.Reset() // +优化
	}
	return req.body
}

var (
	responseBodyPool bytebufferpool.Pool
	requestBodyPool  bytebufferpool.Pool
)

// --- BodyGunzip
// 用于读取那些，有设置'Content-Encoding: gzip'头的body
// 用Body读取gzipped的数据
func (req *Request) BodyGunzip() ([]byte, error) {
	return gunzipData(req.Body())
}
func (resp *Response) BodyGunzip() ([]byte, error) {
	return gunzipData(resp.Body())
}
func gunzipData(p []byte) ([]byte, error) {
	var bb ByteBuffer
	_, err := WriteGunzip(&bb, p)
	if err != nil {
		return nil, err
	}
	return bb.B, nil
}

// --- BodyInflate
// 用于读取那些，有设置'Content-Encoding: defalte'压缩的body
func (req *Request) BodyInflate() ([]byte, error) {
	return inflateData(req.Body())
}
func (resp *Response) BodyInflate() ([]byte, error) {
	return inflateData(resp.Body())
}
func inflateData(p []byte) ([]byte, error) {
	var bb ByteBuffer
	_, err := WriteInflate(&bb, p)
	if err != nil {
		return nil, err
	}
	return bb.B, nil
}

// --- BodyWriteTo
// 1.bodyStream流
// 2.仅MultipartForm
func (req *Request) BodyWriteTo(w io.Writer) error {
	if req.bodyStream != nil {
		_, err := copyZeroAlloc(w, req.bodyStream)
		req.closeBodyStream()
		return err
	}
	if req.onlyMultipartForm() {
		return WriteMultipartForm(w, req.multipartForm, req.multipartFormBoundary)
	}
	_, err := w.Write(req.bodyBytes())
	return err
}
func (resp *Response) BodyWriteTo(w io.Writer) error {
	if resp.bodyStream != nil {
		_, err := copyZeroAlloc(w, resp.bodyStream)
		resp.closeBodyStream()
		return err
	}
	_, err := w.Write(resp.bodyBytes())
	return err
}

// --- Resp.AppendBody
// 函数是复制内容，返回后，p不与body关连
func (resp *Response) AppendBody(p []byte) {
	resp.AppendBodyString(b2s(p))
}
func (resp *Response) AppendBodyString(s string) {
	resp.closeBodyStream()
	resp.bodyBuffer().WriteString(s)
}

// --- Resp.SetBody
// 清空原内容，设置新内容
func (resp *Response) SetBody(body []byte) {
	resp.SetBodyString(b2s(body))
}
func (resp *Response) SetBodyString(body string) {
	resp.closeBodyStream()
	bodyBuf := resp.bodyBuffer()
	bodyBuf.Reset()
	bodyBuf.WriteString(body)
}

// --- Resp.ResetBody
func (resp *Response) ResetBody() {
	resp.closeBodyStream()
	if resp.body != nil {
		if resp.keepBodyBuffer {
			resp.body.Reset()
		} else {
			responseBodyPool.Put(resp.body)
			resp.body = nil
		}
	}
}

// --- ReleaseBody
// 当body容量超过指定大小时，直接释放该body(需不是放回sync.Pool)
// * 旨在让GC回收大内存,需不让该超大块内存，被后续重复使用
// * 必须在ReleaseResponse函数之前使用
// ps: 该函数大部分情况无需使用；使用时，必须明白该函数是如何生效
func (resp *Response) ReleaseBody(size int) {
	if cap(resp.body.B) > size {
		resp.closeBodyStream()
		resp.body = nil
	}
}
func (req *Request) ReleaseBody(size int) {
	if cap(req.body.B) > size {
		req.closeBodyStream()
		req.body = nil
	}
}

// --- SwapBody
// 用指定body替换,并返回原body
// * 传入的body在函数返回后，禁止使用
func (resp *Response) SwapBody(body []byte) []byte {
	bb := resp.bodyBuffer()

	if resp.bodyStream != nil { // 即还未读取内容
		bb.Reset()
		_, err := copyZeroAlloc(bb, resp.bodyStream)
		resp.closeBodyStream()
		if err != nil {
			bb.Reset()
			bb.SetString(err.Error())
		}
	}

	oldBody := bb.B
	bb.B = body
	return oldBody
}
func (req *Request) SwapBody(body []byte) []byte {
	bb := req.bodyBuffer()

	if req.bodyStream != nil {
		bb.Reset()
		_, err := copyZeroAlloc(bb, req.bodyStream)
		req.closeBodyStream()
		if err != nil {
			bb.Reset()
			bb.SetString(err.Error())
		}
	}

	oldBody := bb.B
	bb.B = body
	return oldBody
}

// --- Req.Body
func (req *Request) Body() []byte {
	if req.bodyStream != nil { //若有bodyStream，复制到body中
		bodyBuf := req.bodyBuffer()
		bodyBuf.Reset()
		_, err := copyZeroAlloc(bodyBuf, req.bodyStream)
		req.closeBodyStream()
		if err != nil {
			bodyBuf.SetString(err.Error())
		}
	} else if req.onlyMultipartForm() {
		body, err := marshalMultipartForm(req.multipartForm, req.multipartFormBoundary)
		if err != nil {
			return []byte(err.Error())
		}
		return body
	}
	return req.bodyBytes()
}

// --- Req.AppendBody
func (req *Request) AppendBody(p []byte) {
	req.AppendBodyString(b2s(p))
}
func (req *Request) AppendBodyString(s string) {
	req.RemoveMultipartFormFiles()
	req.closeBodyStream()
	req.bodyBuffer().WriteString(s)
}

// --- Req.SetBody
func (req *Request) SetBody(body []byte) {
	req.SetBodyString(b2s(body))
}
func (req *Request) SetBodyString(body string) {
	req.RemoveMultipartFormFiles()
	req.closeBodyStream()
	req.bodyBuffer().SetString(body)
}

// --- Req.ResetBody
func (req *Request) ResetBody() {
	req.RemoveMultipartFormFiles()
	req.closeBodyStream()
	if req.body != nil {
		if req.keepBodyBuffer {
			req.body.Reset()
		} else {
			requestBodyPool.Put(req.body) //放入前不reset，而是取出时reset:有可能放入后，直接被回收
			req.body = nil
		}
	}
}

// --- CopyTo
func (req *Request) CopyTo(dst *Request) {
	req.copyToSkipBody(dst)
	if req.body != nil {
		dst.bodyBuffer().Set(req.body.B)
	} else if dst.body != nil {
		dst.body.Reset()
	}
}
func (req *Request) copyToSkipBody(dst *Request) {
	dst.Reset()
	req.Header.CopyTo(&dst.Header)

	req.uri.CopyTo(&dst.uri) // uri.header todo??
	dst.parsedURI = req.parsedURI

	req.postArgs.CopyTo(&dst.postArgs)
	dst.parsedPostArgs = req.parsedPostArgs
	dst.isTLS = req.isTLS

	// multipartForm自动生成,在MultipartForm()触发
}

func (resp *Response) CopyTo(dst *Response) {
	resp.copyToSkipBody(dst)
	if resp.body != nil {
		dst.bodyBuffer().Set(resp.body.B)
	} else if dst.body != nil {
		dst.body.Reset()
	}
}
func (resp *Response) copyToSkipBody(dst *Response) {
	dst.Reset()
	resp.Header.CopyTo(&dst.Header)
	dst.SkipBody = resp.SkipBody
}

// ====================================
// --- swapxxxBody
func swapRequestBody(a, b *Request) {
	a.body, b.body = b.body, a.body
	a.bodyStream, b.bodyStream = b.bodyStream, a.bodyStream
}
func swapResponseBody(a, b *Response) {
	a.body, b.body = b.body, a.body
	a.bodyStream, b.bodyStream = b.bodyStream, a.bodyStream
}

// =======================
// --- Req.URI
func (req *Request) URI() *URI {
	req.parseURI()
	return &req.uri
}

func (req *Request) parseURI() {
	if req.parsedURI {
		return
	}
	req.parsedURI = true

	req.uri.parseQuick(req.Header.RequestURI(), &req.Header, req.isTLS)
}

// --- Req.PostArgs
func (req *Request) PostArgs() *Args {
	req.parsePostArgs()
	return &req.postArgs
}
func (req *Request) parsePostArgs() {
	if req.parsedPostArgs {
		return
	}
	req.parsedPostArgs = true

	if !bytes.HasPrefix(req.Header.ContentType(), strPostArgsContentType) {
		return
	}
	req.postArgs.ParseBytes(req.bodyBytes())
}

// --- MultipartForm
var ErrNoMultipartForm = errors.New("request has no multipart/form-data Content-Type")

// 解析multipartForm并返回
// 非multipartForm，返回相应错误
// * RemoveMultipartFormFiles须在返回multipartFrom之后
func (req *Request) MultipartForm() (*multipart.Form, error) {
	if req.multipartForm != nil {
		return req.multipartForm, nil
	}

	req.multipartFormBoundary = string(req.Header.MultipartFormBoundary())
	if len(req.multipartFormBoundary) == 0 { // 未设置分隔符
		return nil, ErrNoMultipartForm
	}

	ce := req.Header.peek(strContentEncoding)
	body := req.bodyBytes()
	if bytes.Equal(ce, strGzip) {
		// 此处不关注内存使用 todo??
		var err error
		if body, err = AppendGunzipBytes(nil, body); err != nil {
			return nil, fmt.Errorf("cannot gunzip request body: %s", err)
		}
	} else if len(ce) > 0 {
		return nil, fmt.Errorf("unsupported Content-Encoding: %q", ce)
	}

	// 使用multipart包读取body中数据
	f, err := readMultipartForm(bytes.NewReader(body), req.multipartFormBoundary, len(body), len(body))
	if err != nil {
		return nil, err
	}
	req.multipartForm = f
	return f, nil
}

// 将multipartForm按格式写入buf中
func marshalMultipartForm(f *multipart.Form, boundary string) ([]byte, error) {
	var buf ByteBuffer
	if err := WriteMultipartForm(&buf, f, boundary); err != nil {
		return nil, err
	}
	return buf.B, nil
}
func WriteMultipartForm(w io.Writer, f *multipart.Form, boundary string) error {
	// 因multipart form处理很慢，此处不关注内存开销 todo??
	if len(boundary) == 0 {
		panic("BUG: form boundary cannot be empty")
	}

	mw := multipart.NewWriter(w)
	if err := mw.SetBoundary(boundary); err != nil {
		return fmt.Errorf("cannot use form boundary %q: %s", boundary, err)
	}

	// 序列化数据
	for k, vv := range f.Value {
		for _, v := range vv {
			if err := mw.WriteField(k, v); err != nil {
				return fmt.Errorf("cannot write form field %q value %q: %s", k, v, err)
			}
		}
	}

	// 序列化文件
	for k, fvv := range f.File {
		for _, fv := range fvv {
			vw, err := mw.CreateFormFile(k, fv.Filename)
			if err != nil {
				return fmt.Errorf("cannot create form file %q (%q): %s", k, fv.Filename, err)
			}
			fh, err := fv.Open()
			if err != nil {
				return fmt.Errorf("cannot open form file %q (%q): %s", k, fv.Filename, err)
			}
			if _, err = copyZeroAlloc(vw, fh); err != nil {
				return fmt.Errorf("error when copying form file %q (%q): %s", k, fv.Filename, err)
			}
			if err = fh.Close(); err != nil {
				return fmt.Errorf("cannot close form file %q (%q): %s", k, fv.Filename, err)
			}
		}
	}

	if err := mw.Close(); err != nil {
		return fmt.Errorf("error when closing multipart form writer: %s", err)
	}

	return nil
}

// 使用multipart包读取数据
func readMultipartForm(r io.Reader, boundary string, size, maxInMemoryFileSize int) (*multipart.Form, error) {
	// 因此处简单比较，要发送的数据大小，所以不关心内存消耗 todo??
	if size <= 0 {
		panic(fmt.Sprintf("BUG: form size must be greater than 0. Given %d", size))
	}
	lr := io.LimitReader(r, int64(size))
	mr := multipart.NewReader(lr, boundary)
	f, err := mr.ReadForm(int64(maxInMemoryFileSize))
	if err != nil {
		return nil, fmt.Errorf("cannot read multipart/form-data body: %s", err)
	}
	return f, nil
}

// --- Req.Reset
func (req *Request) Reset() {
	req.Header.Reset()
	req.resetSkipHeader()
}

func (req *Request) resetSkipHeader() {
	req.ResetBody()
	req.uri.Reset()
	req.parsedURI = false
	req.postArgs.Reset()
	req.parsedPostArgs = false
	req.isTLS = false
}

// --- Req.RemoveMultipartFormFiles
// 移除请求中的临时文件
func (req *Request) RemoveMultipartFormFiles() {
	if req.multipartForm != nil {
		// 不检测错误：这些文件有可能被用户层代码删除、移动到新位置
		req.multipartForm.RemoveAll()
		req.multipartForm = nil
	}
	req.multipartFormBoundary = ""
}

// --- Resp.Reset
func (resp *Response) Reset() {
	resp.Header.Reset()
	resp.resetSkipHeader()
	resp.SkipBody = false
}
func (resp *Response) resetSkipHeader() {
	resp.ResetBody()
}

// --- Req.Read
// 获取请求数据(包括body)
// * RemoveMultipartFormFiles 或 Reset 须在之后调用，以便删除上传的临时文件
// 当MayContinue条件成立，调用者须处理以下任意一条：
// - 1.发送StatusExpectationFailed响应
// - 2.在ContinueReadBody之前，发送StatusContinue响应
// - 3.关闭连接
// ps: 在读取之前，连接关闭时，返回io.EOF
func (req *Request) Read(r *bufio.Reader) error {
	return req.ReadLimitBody(r, 0)
}

const defaultMaxInMemoryFileSize = 16 * 1024 * 1024

var errGetOnly = errors.New("non-GET request received")

// 按限定量，读取body数据
// 当设置了maxBodySize >0,而实际body数据超过该值，返回ErrBodyTooLarge
// * RemoveMultipartFormFiles 或 Reset 须在之后调用，以便删除上传的临时文件
// 当MayContinue条件成立，调用者须处理以下任意一条：
// - 1.发送StatusExpectationFailed响应
// - 2.在ContinueReadBody之前，发送StatusContinue响应
// - 3.关闭连接
// ps: 在读取之前，连接关闭时，返回io.EOF
func (req *Request) ReadLimitBody(r *bufio.Reader, maxBodySize int) error {
	req.resetSkipHeader()
	return req.readLimitBody(r, maxBodySize, false)
}

func (req *Request) readLimitBody(r *bufio.Reader, maxBodySize int, getOnly bool) error {
	// 不在此处reset请求，调用者需自己调用reset

	err := req.Header.Read(r)
	if err != nil {
		return err
	}
	if getOnly && !req.Header.IsGet() {
		return errGetOnly
	}

	if req.Header.noBody() { // HEAD GET方法
		return nil
	}

	if req.MayContinue() {
		// 当有头'Expect: 100-continue'，让调用者决定: 读取body数据或关闭连接
		return nil
	}

	return req.ContinueReadBody(r, maxBodySize)
}

func (req *Request) MayContinue() bool {
	return bytes.Equal(req.Header.peek(strExpect), str100Continue)
}

// 当MayContinue条件成立，调用者须处理以下任意一条：
// - 1.发送StatusExpectationFailed响应
// - 2.在ContinueReadBody之前，发送StatusContinue响应
// - 3.关闭连接
// ps: 在读取之前，连接关闭时，返回io.EOF
func (req *Request) ContinueReadBody(r *bufio.Reader, maxBodySize int) error {
	var err error
	contentLength := req.Header.ContentLength()
	if contentLength > 0 {
		if maxBodySize > 0 && contentLength > maxBodySize {
			return ErrBodyTooLarge
		}

		// 预读取已知长度的multipart form
		// 这里，限制大文件上传的内存使用
		req.multipartFormBoundary = string(req.Header.MultipartFormBoundary())
		if len(req.multipartFormBoundary) > 0 && len(req.Header.peek(strContentEncoding)) == 0 {
			req.multipartForm, err = readMultipartForm(r, req.multipartFormBoundary, contentLength, defaultMaxInMemoryFileSize)
			if err != nil {
				req.Reset()
			}
			return err
		}
	}

	if contentLength == -2 {
		// 因identity的body，是依连接断开作为结尾，与http的requests无关
		// 只需忽略'Content-Length'、'Transfer-Encoding'头
		req.Header.SetContentLength(0)
		return nil
	}

	bodyBuf := req.bodyBuffer()
	bodyBuf.Reset()
	bodyBuf.B, err = readBody(r, contentLength, maxBodySize, bodyBuf.B)
	if err != nil {
		req.Reset()
		return err
	}
	req.Header.SetContentLength(len(bodyBuf.B))
	return nil
}

// --- Resp.Read
// 获取请求数据(包括body)
// * RemoveMultipartFormFiles 或 Reset 须在之后调用，以便删除上传的临时文件
// 当MayContinue条件成立，调用者须处理以下任意一条：
// - 1.发送StatusExpectationFailed响应
// - 2.在ContinueReadBody之前，发送StatusContinue响应
// - 3.关闭连接
// ps: 在读取之前，连接关闭时，返回io.EOF
func (resp *Response) Read(r *bufio.Reader) error {
	return resp.ReadLimitBody(r, 0)
}
func (resp *Response) ReadLimitBody(r *bufio.Reader, maxBodySize int) error {
	resp.resetSkipHeader()
	err := resp.Header.Read(r)
	if err != nil {
		return err
	}
	if resp.Header.StatusCode() == StatusContinue {
		// Read the next response according to http://www.w3.org/Protocols/rfc2616/rfc2616-sec8.html .
		if err = resp.Header.Read(r); err != nil {
			return err
		}
	}

	if !resp.mustSkipBody() {
		bodyBuf := resp.bodyBuffer()
		bodyBuf.Reset()
		bodyBuf.B, err = readBody(r, resp.Header.ContentLength(), maxBodySize, bodyBuf.B)
		if err != nil {
			resp.Reset()
			return err
		}
		resp.Header.SetContentLength(len(bodyBuf.B))
	}
	return nil
}

// HEAD,GET,1xx,204,304
func (resp *Response) mustSkipBody() bool {
	return resp.SkipBody || resp.Header.mustSkipContentLength()
}

var errRequestHostRequired = errors.New("missing required Host header in request")

// --- WriteTo
func (req *Request) WriteTo(w io.Writer) (int64, error) {
	return writeBufio(req, w)
}
func (resp *Response) WriteTo(w io.Writer) (int64, error) {
	return writeBufio(resp, w)
}
func writeBufio(hw httpWriter, w io.Writer) (int64, error) {
	sw := acquireStatsWriter(w)  // 封将写入w方法,添加一个写入字节属性
	bw := acquireBufioWriter(sw) // 使用bufio:写入缓存区
	err1 := hw.Write(bw)         // bufio相关方法
	err2 := bw.Flush()           // bufio相关方法
	releaseBufioWriter(bw)
	n := sw.bytesWritten
	releaseStatsWriter(sw)

	err := err1
	if err == nil {
		err = err2
	}
	return n, err
}

// =====================================
type statsWriter struct {
	w            io.Writer
	bytesWritten int64
}

func (w *statsWriter) Write(p []byte) (int, error) {
	n, err := w.w.Write(p)
	w.bytesWritten += int64(n)
	return n, err
}

func acquireStatsWriter(w io.Writer) *statsWriter {
	v := statsWriterPool.Get()
	if v == nil {
		return &statsWriter{
			w: w,
		}
	}
	sw := v.(*statsWriter)
	sw.w = w
	return sw
}

func releaseStatsWriter(sw *statsWriter) {
	sw.w = nil
	sw.bytesWritten = 0
	statsWriterPool.Put(sw)
}

var statsWriterPool sync.Pool

func acquireBufioWriter(w io.Writer) *bufio.Writer {
	v := bufioWriterPool.Get()
	if v == nil {
		return bufio.NewWriter(w)
	}
	bw := v.(*bufio.Writer)
	bw.Reset(w)
	return bw
}
func releaseBufioWriter(bw *bufio.Writer) {
	bufioWriterPool.Put(bw)
}

var bufioWriterPool sync.Pool

//=======================================
// --- Req.onlyMultipartForm
// 仅multipartForm数据，无body其它数据
func (req *Request) onlyMultipartForm() bool {
	return req.multipartForm != nil && (req.body == nil || len(req.body.B) == 0)
}

// --- Req.Write
// * 考虑性能原因，不直接将request写到w
// ps: WriteTo-bufio缓存方式
func (req *Request) Write(w *bufio.Writer) error {
	if len(req.Header.Host()) == 0 || req.parsedURI { // host是后置触发，需在此处检测，并做相应处理
		uri := req.URI()
		host := uri.Host()
		if len(host) == 0 {
			return errRequestHostRequired
		}
		req.Header.SetHostBytes(host)
		req.Header.SetRequestURIBytes(uri.RequestURI())
	}

	// 若存在bodyStream,则该req是该方式数据
	if req.bodyStream != nil {
		return req.writeBodyStream(w)
	}

	body := req.bodyBytes() //body.B字串
	var err error
	if req.onlyMultipartForm() { // 写入数据，用于发送: 将req设置的边界加入到header
		body, err = marshalMultipartForm(req.multipartForm, req.multipartFormBoundary)
		if err != nil {
			return fmt.Errorf("error when marshaling multipart form: %s", err)
		}
		req.Header.SetMultipartFormBoundary(req.multipartFormBoundary)
	}

	hasBody := !req.Header.noBody()
	if hasBody {
		req.Header.SetContentLength(len(body))
	}
	if err = req.Header.Write(w); err != nil { //header打包成字串，写入到w中
		return err
	}
	if hasBody {
		_, err = w.Write(body)
	} else if len(body) > 0 { //非post请求，body数据不为空
		return fmt.Errorf("non-zero body for non-POST request. body=%q", body)
	}
	return err
}

// --- Resp.WriteGzip
// gzip打包body,并写入到w
// * 写入到w，并设置'Content-Encoding: gzip'头
// 考虑性能原因,不直接写入到w
func (resp *Response) WriteGzip(w *bufio.Writer) error {
	return resp.WriteGzipLevel(w, CompressDefaultCompression)
}

// Level:
// 1.CompressNoCompression
// 2.CompressBestSpeed
// 3.CompressBestCompression
// 4.CompressDefaultCompression
// 5.CompresshuffmanOnly
// * 写入到w，并设置'Content-Encoding: gzip'头
// 考虑性能原因,不直接写入到w
func (resp *Response) WriteGzipLevel(w *bufio.Writer, level int) error {
	if err := resp.gzipBody(level); err != nil {
		return err
	}
	return resp.Write(w)
}

// --- Resp.WriteDeflate
// * 写入到w，并设置'Content-Encoding: deflate'头
// 考虑性能原因,不直接写入到w
func (resp *Response) WriteDeflate(w *bufio.Writer) error {
	return resp.WriteDeflateLevel(w, CompressDefaultCompression)
}

// Level:
// 1.CompressNoCompression
// 2.CompressBestSpeed
// 3.CompressBestCompression
// 4.CompressDefaultCompression
// 5.CompresshuffmanOnly
// * 写入到w，并设置'Content-Encoding: deflate'头
// 考虑性能原因,不直接写入到w
func (resp *Response) WriteDeflateLevel(w *bufio.Writer, level int) error {
	if err := resp.deflateBody(level); err != nil {
		return err
	}
	return resp.Write(w)
}

func (resp *Response) gzipBody(level int) error {
	if len(resp.Header.peek(strContentEncoding)) > 0 {
		// 检测到压缩头，该body有可能已经压缩过
		return nil
	}

	if !resp.Header.isCompressibleContentType() {
		// 该content-type不可压缩
		return nil
	}

	if resp.bodyStream != nil {
		// 因为无法提前知道压缩后的长度，将content-length设为-1(identity)
		// For https://github.com/valyala/fasthttp/issues/176 .
		resp.Header.SetContentLength(-1)

		// 因gzip运行慢，且会分配大量内存，这里忽略内存使用 todo??
		bs := resp.bodyStream
		resp.bodyStream = NewStreamReader(func(sw *bufio.Writer) {
			zw := acquireStacklessGzipWriter(sw, level)
			fw := &flushWriter{
				wf: zw,
				bw: sw,
			}
			copyZeroAlloc(fw, bs)
			releaseStacklessGzipWriter(zw, level)
			if bsc, ok := bs.(io.Closer); ok {
				bsc.Close()
			}
		})
	} else {
		bodyBytes := resp.bodyBytes()
		if len(bodyBytes) < minCompressLen {
			// 无需压缩小body,因为压缩后的数据比未压缩的大
			return nil
		}
		w := responseBodyPool.Get()
		w.Reset() // +优化
		w.B = AppendGzipBytesLevel(w.B, bodyBytes, level)

		// Hack: swap resp.body with w
		if resp.body != nil {
			responseBodyPool.Put(resp.body)
		}
		resp.body = w
	}
	resp.Header.SetCanonical(strContentEncoding, strGzip)
	return nil
}

func (resp *Response) deflateBody(level int) error {
	if len(resp.Header.peek(strContentEncoding)) > 0 {
		// 检测到压缩头，该body有可能已经压缩过
		return nil
	}

	if !resp.Header.isCompressibleContentType() {
		// 该content-type不可压缩
		return nil
	}

	if resp.bodyStream != nil {
		// 因为无法提前知道压缩后的长度，将content-length设为-1(identity)
		// For https://github.com/valyala/fasthttp/issues/176 .
		resp.Header.SetContentLength(-1)

		// 因gzip运行慢，且会分配大量内存，这里忽略内存使用 todo??
		bs := resp.bodyStream
		resp.bodyStream = NewStreamReader(func(sw *bufio.Writer) {
			zw := acquireStacklessDeflateWriter(sw, level) //接入压缩接口
			fw := &flushWriter{
				wf: zw,
				bw: sw,
			}
			copyZeroAlloc(fw, bs) // bs->fw:通过缓冲区方式，复制数据->io.CopyBuffer
			releaseStacklessDeflateWriter(zw, level)
			if bsc, ok := bs.(io.Closer); ok { // 关闭流
				bsc.Close()
			}
		})
	} else {
		bodyBytes := resp.bodyBytes()
		if len(bodyBytes) < minCompressLen {
			// 无需压缩小body,因为压缩后的数据比未压缩的大
			return nil
		}
		w := responseBodyPool.Get()
		w.Reset() // +优化
		w.B = AppendDeflateBytesLevel(w.B, bodyBytes, level)

		// Hack: swap resp.body with w.
		if resp.body != nil {
			responseBodyPool.Put(resp.body)
		}
		resp.body = w
	}
	resp.Header.SetCanonical(strContentEncoding, strDeflate)
	return nil
}

// body长度小于minCompressLen,不进行压缩
const minCompressLen = 200

//
type writeFlusher interface {
	io.Writer
	Flush() error
}
type flushWriter struct {
	wf writeFlusher
	bw *bufio.Writer
}

// 封装bufio的Write和Flush操作
func (w *flushWriter) Write(p []byte) (int, error) {
	n, err := w.wf.Write(p)
	if err != nil {
		return 0, err
	}
	if err = w.wf.Flush(); err != nil {
		return 0, err
	}
	if err = w.bw.Flush(); err != nil {
		return 0, err
	}
	return n, nil
}

// --- Resp.Write
// 考虑性能原因，不直接写入w
func (resp *Response) Write(w *bufio.Writer) error {
	sendBody := !resp.mustSkipBody()

	if resp.bodyStream != nil {
		return resp.writeBodyStream(w, sendBody)
	}

	body := resp.bodyBytes()
	bodyLen := len(body)
	if sendBody /* || bodyLen > 0*/ { // todo??
		resp.Header.SetContentLength(bodyLen)
	}
	if err := resp.Header.Write(w); err != nil {
		return err
	}
	if sendBody {
		if _, err := w.Write(body); err != nil {
			return err
		}
	}
	return nil
}

// --- writeBodyStream
// chunk条件:
// * 数据长度不大于int -- 想体验好，需相应改小
// * 流方式
func (req *Request) writeBodyStream(w *bufio.Writer) error {
	var err error

	contentLength := req.Header.ContentLength() //预设的长度
	if contentLength < 0 {
		lrSize := limitedReaderSize(req.bodyStream) //获取流长度
		if lrSize >= 0 {
			contentLength = int(lrSize)
			if int64(contentLength) != lrSize { //确认长度有没超过int最大值，超过为-1
				contentLength = -1
			}
			if contentLength >= 0 {
				req.Header.SetContentLength(contentLength)
			}
		}
	}
	if contentLength >= 0 {
		if err = req.Header.Write(w); err == nil {
			err = writeBodyFixedSize(w, req.bodyStream, int64(contentLength))
		}
	} else { //分段
		req.Header.SetContentLength(-1)
		if err = req.Header.Write(w); err == nil {
			err = writeBodyChunked(w, req.bodyStream)
		}
	}
	err1 := req.closeBodyStream()
	if err == nil {
		err = err1
	}
	return err
}

func (resp *Response) writeBodyStream(w *bufio.Writer, sendBody bool) error {
	var err error

	contentLength := resp.Header.ContentLength() //预设的长度
	if contentLength < 0 {
		lrSize := limitedReaderSize(resp.bodyStream) //获取流长度
		if lrSize >= 0 {
			contentLength = int(lrSize)
			if int64(contentLength) != lrSize { //确认长度有没超过int最大值，超过为-1
				contentLength = -1
			}
			if contentLength >= 0 {
				resp.Header.SetContentLength(contentLength)
			}
		}
	}
	if contentLength >= 0 {
		if err = resp.Header.Write(w); err == nil && sendBody {
			err = writeBodyFixedSize(w, resp.bodyStream, int64(contentLength))
		}
	} else {
		resp.Header.SetContentLength(-1)
		if err = resp.Header.Write(w); err == nil && sendBody {
			err = writeBodyChunked(w, resp.bodyStream)
		}
	}
	err1 := resp.closeBodyStream()
	if err == nil {
		err = err1
	}
	return err
}

// --- closeBodyStream
func (req *Request) closeBodyStream() error {
	if req.bodyStream == nil {
		return nil
	}
	var err error
	if bsc, ok := req.bodyStream.(io.Closer); ok {
		err = bsc.Close()
	}
	req.bodyStream = nil
	return err
}

func (resp *Response) closeBodyStream() error {
	if resp.bodyStream == nil {
		return nil
	}
	var err error
	if bsc, ok := resp.bodyStream.(io.Closer); ok {
		err = bsc.Close()
	}
	resp.bodyStream = nil
	return err
}

// --- String
// 有错误时，返回错误信息
// 如有性能考虑，使用Write
func (req *Request) String() string {
	return getHTTPString(req)
}
func (resp *Response) String() string {
	return getHTTPString(resp)
}

// ====================================
func getHTTPString(hw httpWriter) string {
	w := AcquireByteBuffer()
	w.Reset() //+优化
	bw := bufio.NewWriter(w)
	if err := hw.Write(bw); err != nil {
		return err.Error()
	}
	if err := bw.Flush(); err != nil {
		return err.Error()
	}
	s := string(w.B)
	ReleaseByteBuffer(w)
	return s
}

type httpWriter interface {
	Write(w *bufio.Writer) error
}

// 通过缓存方式，分段将r数据，写入w中
func writeBodyChunked(w *bufio.Writer, r io.Reader) error {
	vbuf := copyBufPool.Get()
	buf := vbuf.([]byte)

	var err error
	var n int
	for {
		n, err = r.Read(buf)
		if n == 0 {
			if err == nil {
				panic("BUG: io.Reader returned 0, nil")
			}
			if err == io.EOF { //读取末尾
				if err = writeChunk(w, buf[:0]); err != nil { //写0，表示chunk结束
					break
				}
				err = nil
			}
			break
		}
		if err = writeChunk(w, buf[:n]); err != nil {
			break
		}
	}

	copyBufPool.Put(vbuf)
	return err
}

func limitedReaderSize(r io.Reader) int64 {
	lr, ok := r.(*io.LimitedReader)
	if !ok {
		return -1
	}
	return lr.N
}

func writeBodyFixedSize(w *bufio.Writer, r io.Reader, size int64) error {
	if size > maxSmallFileSize {
		if err := w.Flush(); err != nil {
			// w 的缓存区须为空，用于触发 发送文件-bufio.Writer.ReadFrom
			return err
		}
	}

	// 提取简单的limited reader,用于触发，发送文件-net.TcpConn.ReadFrom
	lr, ok := r.(*io.LimitedReader)
	if ok {
		r = lr.R
	}

	n, err := copyZeroAlloc(w, r)

	if ok {
		lr.N -= n
	}

	if n != size && err == nil { //实际复制数据，与传入大小对不上
		err = fmt.Errorf("copied %d bytes from body stream instead of %d bytes", n, size)
	}
	return err
}

// 通过缓冲区方式，复制数据
func copyZeroAlloc(w io.Writer, r io.Reader) (int64, error) {
	vbuf := copyBufPool.Get()
	buf := vbuf.([]byte)
	n, err := io.CopyBuffer(w, r, buf) //使用buf作为缓冲区，把r数据复制到w中-若buf=nil,则会申请一个临时缓冲区
	copyBufPool.Put(vbuf)
	return n, err
}

// 4k缓冲池
var copyBufPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 4096)
	},
}

// 将b写入w中:http协议中的chunk写入方式
// 在一个连接中chunk-flush数据
// 1.十六进制长度
// 2.CRLF
// 3.内容
// 4.CRLF
func writeChunk(w *bufio.Writer, b []byte) error {
	n := len(b)
	writeHexInt(w, n) //十六进制方式写入长度
	w.Write(strCRLF)
	w.Write(b)
	_, err := w.Write(strCRLF)
	err1 := w.Flush() //刷数据到对端
	if err == nil {
		err = err1
	}
	return err
}

var ErrBodyTooLarge = errors.New("body size exceeds the given limit")

// 读取body
// 1.有明确指定contentlength
// 2.content=-1,chunked
// 3.从头接到尾的流
func readBody(r *bufio.Reader, contentLength int, maxBodySize int, dst []byte) ([]byte, error) {
	dst = dst[:0]
	if contentLength >= 0 {
		if maxBodySize > 0 && contentLength > maxBodySize {
			return dst, ErrBodyTooLarge
		}
		return appendBodyFixedSize(r, dst, contentLength)
	}
	if contentLength == -1 {
		return readBodyChunked(r, maxBodySize, dst)
	}
	return readBodyIdentity(r, maxBodySize, dst)
}

// 通过缓冲区dst(限定大小),读取r的内容
// 读取过程中，检测最大限制(默认4k)
func readBodyIdentity(r *bufio.Reader, maxBodySize int, dst []byte) ([]byte, error) {
	dst = dst[:cap(dst)]
	if len(dst) == 0 {
		dst = make([]byte, 1024)
	}
	offset := 0 //从第1字节开始
	for {
		nn, err := r.Read(dst[offset:])
		if nn <= 0 {
			if err != nil {
				if err == io.EOF {
					return dst[:offset], nil // 读到末尾
				}
				return dst[:offset], err
			}
			panic(fmt.Sprintf("BUG: bufio.Read() returned (%d, nil)", nn))
		}
		offset += nn
		if maxBodySize > 0 && offset > maxBodySize { // 检测是否超过最大值
			return dst[:offset], ErrBodyTooLarge
		}
		if len(dst) == offset { // 读缓冲区满，重新分配缓存区:2倍方式,最大在maxBodySize+1
			n := round2(2 * offset)
			if maxBodySize > 0 && n > maxBodySize {
				n = maxBodySize + 1
			}
			b := make([]byte, n)
			copy(b, dst)
			dst = b
		}
	}
}

// 读取指定长度数据到dst
func appendBodyFixedSize(r *bufio.Reader, dst []byte, n int) ([]byte, error) {
	if n == 0 {
		return dst, nil
	}

	offset := len(dst)
	dstLen := offset + n
	if cap(dst) < dstLen {
		b := make([]byte, round2(dstLen))
		copy(b, dst)
		dst = b
	}
	dst = dst[:dstLen]

	for {
		nn, err := r.Read(dst[offset:])
		if nn <= 0 {
			if err != nil {
				if err == io.EOF {
					err = io.ErrUnexpectedEOF
				}
				return dst[:offset], err
			}
			panic(fmt.Sprintf("BUG: bufio.Read() returned (%d, nil)", nn))
		}
		offset += nn
		if offset == dstLen {
			return dst, nil
		}
	}
}

// 读取chunk数据
// 1.十六进制长度
// 2.crlf
// 3.内容
// 4.crlf
func readBodyChunked(r *bufio.Reader, maxBodySize int, dst []byte) ([]byte, error) {
	if len(dst) > 0 {
		panic("BUG: expected zero-length buffer")
	}

	strCRLFLen := len(strCRLF)
	for {
		chunkSize, err := parseChunkSize(r) // 找到chunk长度
		if err != nil {
			return dst, err
		}
		if maxBodySize > 0 && len(dst)+chunkSize > maxBodySize { //超过最大限制
			return dst, ErrBodyTooLarge
		}
		dst, err = appendBodyFixedSize(r, dst, chunkSize+strCRLFLen) // 读取指定长度数据
		if err != nil {
			return dst, err
		}
		if !bytes.Equal(dst[len(dst)-strCRLFLen:], strCRLF) { // 需以crlf结尾
			return dst, fmt.Errorf("cannot find crlf at the end of chunk")
		}
		dst = dst[:len(dst)-strCRLFLen]
		if chunkSize == 0 { // 读取到最后一chunk
			return dst, nil
		}
	}
}

// 读取: HexInt+CRLF
func parseChunkSize(r *bufio.Reader) (int, error) {
	n, err := readHexInt(r)
	if err != nil {
		return -1, err
	}
	c, err := r.ReadByte() //取1字节
	if err != nil {
		return -1, fmt.Errorf("cannot read '\r' char at the end of chunk size: %s", err)
	}
	if c != '\r' {
		return -1, fmt.Errorf("unexpected char %q at the end of chunk size. Expected %q", c, '\r')
	}
	c, err = r.ReadByte() //取1字节
	if err != nil {
		return -1, fmt.Errorf("cannot read '\n' char at the end of chunk size: %s", err)
	}
	if c != '\n' {
		return -1, fmt.Errorf("unexpected char %q at the end of chunk size. Expected %q", c, '\n')
	}
	return n, nil
}

// 按2进制取整
// 1.<= 0 转化成 0
// 2.右移直到0
// 3.1左移第'2'求得的位数
func round2(n int) int {
	if n <= 0 {
		return 0
	}
	n-- //用1作基数，此处减1
	x := uint(0)
	for n > 0 {
		n >>= 1
		x++
	}
	return 1 << x
}
