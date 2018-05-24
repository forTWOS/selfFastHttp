package selfFastHttp

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"time"
)

// 禁止直接复制值,正确做法:新建并CopyTo
// 不可用于并发
// -- 接口：Add(Byte[K][V]),Set(Byte[K][V]),Del(Bytes),VisitAll,Write(To),CopyTo,Peek(Bytes),Read,Reset,String,Header
type RequestHeader struct {
	noCopy noCopy

	disableNormalizing bool // 禁止格式化头部各字段名;默认启用-首字母大小
	noHTTP11           bool // 请求是否是 HTTP/1.1
	connectionClose    bool // 是否已设'Connection: close'响应头
	isGet              bool // 请求方法是否是 Get 方式

	// 这两个bool移到此处，是为了贴近其它bool，减少RequestHeader大小
	cookiesCollected bool // 从h中收集cookies
	rawHeadersParsed bool // 是否解析了原始头部

	contentLength      int    // 用于响应头: 'Range: bytes=startPos-endPos/contentLength'
	contentLengthBytes []byte // contentLength的字节化

	method      []byte // PUT HEAD DELETE GET POST
	requestURI  []byte
	host        []byte // www.google.com
	contentType []byte // 无用 网页编码:text/plain; charset=utf-8
	userAgent   []byte // 'User-Agent:Mozilla/5.0 (Windows NT 6.1; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/56.0.2924.87 Safari/537.36'

	h     []argsKV // 头部数据-键值对
	bufKV argsKV   // 内部[]byte工具,减少临时变量使用

	cookies []argsKV // 与cookiesCollected合用

	rawHeaders []byte // 与rawHeadersParsed合用

}

// 禁止直接复制值,正确做法:新建并CopyTo
// 不可用于并发
type ResponseHeader struct {
	noCopy noCopy

	disableNormalizing bool // 禁止格式化头部各字段名;默认启用-首字母大小
	noHTTP11           bool // 请求是否是 HTTP/1.1
	connectionClose    bool // 是否已设'Connection: close'响应头

	statusCode         int    // 响应状态码
	contentLength      int    // 用于响应头: 'Range: bytes=startPos-endPos/contentLength'
	contentLengthBytes []byte // contentLength的字节化

	contentType []byte // 网页编码:text/plain; charset=utf-8
	server      []byte // 'Server: Microsoft-IIS/6.0' 'Server: nginx'

	h     []argsKV // 头部数据-键值对
	bufKV argsKV   // 内部[]byte工具,减少临时变量使用

	cookies []argsKV // 与cookiesCollected合用 i.e.'Set-Cookie: __cfduid=dbab338f0f87894a976; expires=Thu, 23-May-19 03:06:16 GMT; path=/; domain=.cyberciti.org; HttpOnly'
}

//=========================================
// 'Content-Range: bytes startPos-endPos/contentLength'
// 用于响应分段请求数据
func (h *ResponseHeader) SetContentRange(startPos, endPos, contentLength int) {
	b := h.bufKV.value[:0]
	b = append(b, strBytes...)
	b = append(b, ' ')
	b = AppendUint(b, startPos)
	b = append(b, '-')
	b = AppendUint(b, endPos)
	b = append(b, '/')
	b = AppendUint(b, contentLength)
	h.bufKV.value = b

	h.SetCanonical(strContentRange, h.bufKV.value)
}

// 分段请求数据-断点续传
// 值根据响应的: Content-Length和Accept-Ranges确定
// 'Range: bytes=startPos-endPos'
// * 当startPos为负，'bytes=-startPos'
// * 当endPos为负，将忽略endPos, 形如'bytes=startPos-'
func (h *RequestHeader) SetByteRange(startPos, endPos int) {
	h.parseRawHeaders()

	b := h.bufKV.value[:0]
	b = append(b, strBytes...)
	b = append(b, '=')
	if startPos >= 0 {
		b = AppendUint(b, startPos)
	} else {
		endPos = -startPos
	}
	b = append(b, '-')
	if endPos >= 0 {
		b = AppendUint(b, endPos)
	}
	h.bufKV.value = b
	h.SetCanonical(strRange, h.bufKV.value)
}

//---------------------------------------
// 响应状态码，默认200
func (h *ResponseHeader) StatusCode() int {
	if h.statusCode == 0 {
		return StatusOK
	}
	return h.statusCode
}
func (h *ResponseHeader) SetStatusCode(statusCode int) {
	h.statusCode = statusCode
}

// 'Last-Modified'
func (h *ResponseHeader) SetLastModified(t time.Time) {
	h.bufKV.value = AppendHTTPDate(h.bufKV.value[:0], t)
	h.SetCanonical(strLastModified, h.bufKV.value)
}

// 是否设置'Connection: close'
func (h *ResponseHeader) ConnectionClose() bool {
	return h.connectionClose
}
func (h *ResponseHeader) SetConnectionClose() {
	h.connectionClose = true
}

// 当设置了'Connection: close'，清除之
func (h *ResponseHeader) ResetConnectionClose() {
	if h.connectionClose {
		h.connectionClose = false
		h.h = delAllArgsBytes(h.h, strConnection)
	}
}

// 是否设置:'Connection: close'
func (h *RequestHeader) ConnectionClose() bool {
	h.parseRawHeader()
	return h.connectionClose
}

// 简化版-按需
func (h *RequestHeader) ConnectionCloseFast() bool {
	return h.connectionClose
}

func (h *RequestHeader) SetConnectionClose() {
	// 考虎性能原因，未设用h.parseRawHeaders()
	h.connectionClose = true
}

// 如果已设'Connection: close',清理之
func (h *RequestHeader) ReSetConnectionClose() {
	h.parseRawHeader()
	if h.connectionClose {
		h.connectionClose = false
		h.h = delAllArgsBytes(h.h, strConnection)
	}
}

//---------------------------------------
// --- 'Connection: Upgrade'
func (h *ResponseHeader) ConnectionUpgrade() bool {
	return hasHeaderValue(h.Peek("Connection"), strUpgrade)
}
func (h *RequestHeader) ConnectionUpgrade() bool {
	h.parseRawHeaders()
	return hasHeaderValue(h.Peek("Connection"), strUpgrade)
}

//----------------
// --- 'Content-Length: xx'
// -1 : 'Transfer-Encoding: chunked' 十六进制的长度值和数据，长度值在其后独占一行，长度不包括它结尾的 CRLF(\r\n)，也不包括分块数据结尾的 CRLF
// -2 : 'Transfer-Encoding: identity' HTTP/1.1已弃 从第一个字按顺序传输到最后一个字结束
func (h *ResponseHeader) ContentLength() int {
	return h.contentLength
}

func (h *ResponseHeader) SetContentLength(contentLength int) {
	if h.mustSkipContentLength() {
		return
	}
	h.contentLength = contentLength
	if contentLength >= 0 {
		h.contentLengthBytes = AppendUint(h.contentLengthBytes[:0], contentLength)
		h.h = delAllArgsBytes(h.h, strTransferEncoding)
	} else {
		h.contentLengthBytes = h.contentLengthBytes[:0]
		value := strChunked
		if contentLength == -2 {
			h.SetConnectionClose()
			value = strIdentity
		}
		h.h = setArgBytes(h.h, strTransferEncoding, value)
	}
}

// 是否须不设置ContentLength
// 1xx(提示信息), 204(无内容), 304(无修改) 必定不设置
func (h *ResponseHeader) mustSkipContentLength() bool {
	statusCode := h.StatusCode()

	// 快速判断
	if statusCode < 100 || statusCode == StatusOK {
		return false
	}

	// slow
	return statusCode == StatusNotModified || statusCode == StatusNoContent || statusCode < 200
}

func (h *RequestHeader) ContentLength() int {
	if h.noBody() {
		return 0
	}
	h.parseRawHeaders()
	return h.contentLength
}

// 为负: 设置'Transfer-Encoding: chunked'
func (h *RequestHeader) SetContentLength(contentLength int) {
	h.parseRawHeaders()
	h.contentLength = contentLength
	if contentLength >= 0 {
		h.contentLengthBytes = AppendUint(h.contentLengthBytes[:0], contentLength)
		h.h = delAllArgsBytes(h.h, strTransferEncoding)
	} else {
		h.contentLengthBytes = h.contentLengthBytes[:0]
		h.h = setArgBytes(h.h, strTransferEncoding, strChunked)
	}
}

//--------------------
// --- ContentType
// 是否可压缩body: 是否是'text/'or'application/'打头的,是即可压缩
func (h *ResponseHeader) isCompressiableContentType() bool {
	contentType := h.ContentType()
	return bytes.HasPrefix(contentType, strTextSlash) ||
		bytes.HasPrefix(contentType, strApplicationSlash)
}

// 返回ContentType,未设置，使用默认值
func (h *ResponseHeader) ContentType() []byte {
	contentType := h.contentType
	if len(contentType) == 0 {
		contentType = defaultContentType
	}
	return contentType
}

func (h *ResponseHeader) SetContentType(contentType string) {
	h.contentType = append(h.contentType[:0], contentType...)
}

func (h *ResponseHeader) SetContentTypeBytes(contentType []byte) {
	h.contentType = append(h.contentType[:0], contentType...)
}

// --- Resp.Server
func (h *ResponseHeader) Server() []byte {
	return h.server
}

func (h *ResponseHeader) SetServer(server string) {
	h.server = append(h.server[:0], server...)
}

func (h *ResponseHeader) SetServerBytes(server []byte) {
	h.server = append(h.server[:0], server...)
}

// --- Req.ContentType
func (h *RequestHeader) ContentType() []byte {
	h.parseRawHeaders()
	return h.contentType
}

func (h *RequestHeader) SetContentType(contentType string) {
	h.parseRawHeaders()
	h.contentType = append(h.contentType[:0], contentType...)
}
func (h *RequestHeader) SetContentTypeBytes(contentType []byte) {
	h.parseRawHeaders()
	h.contentType = append(h.contentType[:0], contentType...)
}

// --- Req.boundary
// 'Content-Type: multiform/form-data; boundary=...'
func (h *RequestHeader) SetMultipartFormBoundary(boundary string) {
	h.parseRawHeader()

	b := h.bufKV.value[:0]
	b = append(b, strMultipartFormData...)
	b = append(b, ';', ' ')
	b = append(b, strBoundary...)
	b = append(b, '=')
	b = append(b, boundary...)
	h.bufKV.value = b

	h.SetContentTypeBytes(h.bufKV.value)
}

func (h *RequestHeader) SetMultipartFormBoundaryBytes(boundary []byte) {
	h.parseRawHeader()

	b := h.bufKV.value[:0]
	b = append(b, strMultipartFormData...)
	b = append(b, ';', ' ')
	b = append(b, strBoundary...)
	b = append(b, '=')
	b = append(b, boundary...)
	h.bufKV.value = b

	h.SetContentTypeBytes(h.bufKV.value)
}

// 从'Content-Type: multiform/form-data; boundary=...'返回
func (h *RequestHeader) MultipartFormBoundary() []byte {
	b := h.COntentType()
	if !bytes.HasPrefix(b, strMultipartFormData) {
		return nil
	}
	b = b[len(strMultipartFormData):]
	if len(b) == 0 || b[0] != ';' {
		return nil
	}

	var n int
	for len(b) > 0 {
		n++
		for len(b) > n && b[n] == ' ' {
			n++
		}
		b = b[n:]
		if !bytes.HasPrefix(b, strBoundary) { // 查找'boundary'关键字
			if n = bytes.IndexByte(b, ';'); n < 0 { // 按标准，仅1个';',若未找到，表示后续不存在该值
				return nil
			}
			continue
		}

		b = b[len(strBoundary):]
		if len(b) == 0 || b[0] != '=' { //空值或非法
			return nil
		}
		b = b[1:]
		if n = bytes.IndexByte(b, ';'); n >= 0 { //若有';'结尾字符，截之
			b = b[:n]
		}
		if len(b) > 1 && b[0] == '"' && b[len(b)-1] == '"' { //去除头尾的 双引号
			b = b[1 : len(b)-1]
		}
		return b
	}
	return nil
}

// --- Req.Host
func (h *RequestHeader) Host() []byte {
	if len(h.host) > 0 {
		return h.host
	}
	// 未解析，在原始字串中取值
	if !h.rawHeadersParsed {
		// 快速通通：不通过解析完整header
		host := peekRawHeader(h.rawHeaders, strHost)
		if len(host) > 0 {
			h.host = append(h.host[:0], host...)
			return h.host
		}
	}

	// 慢速通道
	h.parseRawHeaders()
	return h.host
}

func (h *RequestHeader) SetHost(host string) {
	h.parseRawHeaders()
	h.host = append(h.host[:0], host...)
}
func (h *RequestHeader) SetHostBytes(host []byte) {
	h.parseRawHeaders()
	h.host = append(h.host[:0], host...)
}

// --- Req.UserAgent
func (h *RequestHeader) UserAgent() []byte {
	h.parseRawHeaders()
	return h.userAgent
}

func (h *RequestHeader) SetUserAgent(userAgent string) {
	h.parseRawHeaders()
	h.userAgent = append(h.userAgent[:0], userAgent...)
}
func (h *RequestHeader) SetUserAgentBytes(userAgent string) {
	h.parseRawHeaders()
	h.userAgent = append(h.userAgent[:0], userAgent...)
}

// --- Req.Referer
func (h *RequestHeader) Referer() []byte {
	return h.PeekBytes(strReferer)
}

func (h *RequestHeader) SetReferer(referer string) {
	h.SetBytesK(strReferer, referer)
}

func (h *RequestHeader) SetRefererBytes(referer []byte) {
	h.SetCanonical(strReferer, referer)
}

// --- Req.Method
// 默论Get方式
func (h *RequestHeader) Method() []byte {
	if len(h.method) == 0 {
		return strGet
	}
}

func (h *RequestHeader) SetMethod(method string) {
	h.method = append(h.method[:0], method...)
}
func (h *RequestHeader) SetMethodBytes(method string) {
	h.method = append(h.method[:0], method...)
}

// --- Req.RequestURI
// 默认'/',仅URI
func (h *RequestHeader) RequestURI() []byte {
	requestURI := h.requestURI
	if len(requestURI) == 0 {
		requestURI = strSlash
	}
	return requestURI
}

// 将在HTTP请求第1行设置RequestURI
// RequestURI须是经过转码的
// 在并发中使用，结果将不可知
func (h *RequestHeader) SetRequestURI(requestURI string) {
	h.requestURI = append(h.requestURI[:0], requestURI...)
}
func (h *RequestHeader) SetRequestURIBytes(requestURI []byte) {
	h.requestURI = append(h.requestURI[:0], requestURI...)
}

// --- Method: Isxx()
func (h *RequestHeader) IsGet() bool {
	if !h.isGet { // 快速
		h.isGet = bytes.Equal(h.Method(), strGet)
	}
	return h.isGet
}

func (h *RequestHeader) IsPost() bool {
	return bytes.Equal(h.Method(), strPost)
}

func (h *RequestHeader) IsPut() bool {
	return bytes.Equal(h.Method(), strPut)
}

func (h *RequestHeader) IsHead() bool {
	if h.isGet { // 快速
		return false
	}
	return bytes.Equal(h.Method(), strHead)
}

func (h *RequestHeader) IsDelete() bool {
	return bytes.Equal(h.Method(), strDelete)
}

// --- IsHTTP11
func (h *RequestHeader) IsHTTP11() bool {
	return !h.noHTTP11
}
func (h *ResponseHeader) IsHTTP11() bool {
	return !h.noHTTP11
}

// --- Req.AcceptEncoding: 是否显式设置了支持该编码
// Examples:
//  Accept-Encoding: compress, gzip　　　　　　　　　　　　//支持compress 和gzip类型
//  Accept-Encoding:　　　　　　　　　　　　　　　　　　　　//默认是identity
//  Accept-Encoding: *　　　　　　　　　　　　　　　　　　　　//支持所有类型
//  Accept-Encoding: compress;q=0.5, gzip;q=1.0　　　　　　//按顺序支持 gzip , compress
//  Accept-Encoding: gzip;q=1.0, identity;q=0.5, *;q=0       // 按顺序支持 gzip , identity
// Server对应响应: 'Content-Encoding: gzip'
// ps: q值的范围从0.0~1.0（1.0优先级最高）
func (h *RequestHeader) HasAcceptEncoding(acceptEncoding string) bool {
	h.bufKV.value = append(h.bufKV.value[:0], acceptEncoding...)
	return h.HasAcceptEncodingBytes(h.bufKV.value)
}

func (h *RequestHeader) HasAcceptEncodingBytes(acceptEncoding []byte) bool {
	ae := h.peek(strAcceptEncoding)
	n := bytes.Index(ae, acceptEncoding)
	if n < 0 || ae[n-1] != ' ' { // 该字串前1位，是否是分隔符' '
		return false
	}
	b := ae[n+len(acceptEncoding):]
	if len(b) > 0 && b[0] != ',' { // 若后续字串有值，且不是',' => 不包含
		// fast && crude: identity;q=0,
		if len(b) >= 4 && b[0] == ';' && b[1] == 'q' {
			if len(b) == 4 && b[3] == '0' ||
				len(b) > 4 && b[3] == '0' && b[4] == ',' {
				return false
			} else {
				return true
			}
		}
		return false
	}
	if n == 0 {
		return true
	}
	return true
}

// --- Resp.Length of 响应头选项
func (h *ResponseHeader) Len() int {
	n := 0
	h.VisitAll(func(k, v []byte) { n++ })
	return n
}
func (h *RequestHeader) Len() int {
	n := 0
	h.VisitAll(func(k, v []byte) { n++ })
	return n
}

// --- disableNormalizing
// 禁止标准化 头选项名
// 默认开启(首字母大写)
func (h *ResponseHeader) DisableNormalizing() {
	h.disableNormalizing = true
}
func (h *RequestHeader) DisableNormalizing() {
	h.disableNormalizing = true
}

// --- Reset
// 清理:响应头
func (h *ResponseHeader) Reset() {
	h.disableNormalizing = false
	h.resetSkipNormalize()
}

func (h *ResponseHeader) resetSkipNormalize() {
	h.noHTTP11 = false
	h.connectionClose = false

	h.statusCode = 0 // StatusOk
	h.contentLength = 0
	h.contentLengthBytes = h.contentLengthBytes[:0]

	h.contentType = h.contentType[:0]
	h.server = h.server[:0]

	h.h = h.h[:0]
	h.cookies = h.cookies[:0]
}
func (h *RequestHeader) Reset() {
	h.disableNormalizing = false
	h.resetSkipNormalize()
}

func (h *RequestHeader) resetSkipNormalize() {
	h.noHTTP11 = false
	h.connectionClose = false
	h.isGet = false

	h.contentLength = 0
	h.contentLengthBytes = h.contentLengthBytes[:0]

	h.method = h.method[:0]
	h.requestURI = h.requestURI[:0]
	h.host = h.host[:0]
	h.contentType = h.contentType[:0]
	h.userAgent = h.userAgent[:0]

	h.h = h.h[:0]
	h.cookies = h.cookies[:0]
	h.cookiesCollected = false

	h.rawHeaders = h.rawHeaders[:0]
	h.rawHeadersParsed = false
}

// --- CopyTo
func (h *ResponseHeader) CopyTo(dst *ResponseHeader) {
	dst.Reset()

	dst.disableNormalizing = h.disableNormalizing
	dst.noHTTP11 = h.noHTTP11
	dst.connectionClose = h.connectionClose

	dst.statusCode = h.statusCode
	dst.contentLength = h.contentLength
	dst.contentLengthBytes = append(dst.contentLengthBytes[:0], h.contentLengthBytes...)

	dst.contentType = append(dst.contentType[:0], h.contentType...)
	dst.server = append(dst.server[:0], h.server...)

	dst.h = copyArgs(dst.h, h.h)
	dst.cookies = copy(dst.cookies, h.cookies)
}

func (h *RequestHeader) CopyTo(dst *RequestHeader) {
	dst.Reset()

	dst.disableNormalizing = h.disableNormalizing
	dst.noHTTP11 = h.noHTTP11
	dst.connectionClose = h.connectionClose
	dst.isGet = h.isGet

	dst.contentLength = h.contentLength
	dst.contentLengthBytes = append(dst.contentLengthBytes[:0], h.contentLengthBytes...)

	dst.method = append(dst.method[:0], h.method...)
	dst.requestURI = append(dst.requestURI[:0], h.requestURI...)
	dst.host = append(dst.host[:0], h.host...)
	dst.contentType = append(dst.contentType[:0], h.contentType...)
	dst.userAgent = append(dst.userAgent[:0], h.userAgent...)

	dst.h = copyArgs(dst.h, h.h)
	dst.cookies = copyArgs(dst.cookies, h.cookies)
	dst.cookiesCollected = h.cookiesCollected

	dst.rawHeaders = append(dst.rawHeaders[:0], h.rawHeaders...)
	dst.rawHeadersParsed = false
}

// --- VisitAll
// f不要直接保存传入的值，需要使用复制
func (h *ResponseHeader) VisitAll(f func(key, value []byte)) {
	if len(h.contentLengthBytes) > 0 {
		f(strContentLength, h.contentLengthBytes)
	}
	contentType := h.ContentType()
	if len(contentType) > 0 {
		f(strContentType, contentType)
	}
	server := h.Server()
	if len(server) > 0 {
		f(strServer, server)
	}
	if len(h.cookies) > 0 {
		visitArgs(h.cookies, func(k, v []byte) {
			f(strSetCookie, v)
		})
	}
	visitArgs(h.h, f)
	if h.ConnectionClose() {
		f(strConnection, strClose)
	}
}
func (h *RequestHeader) VisitAll(f func(key, value []byte)) {
	h.parseRawHeader()
	host := h.Host()
	if len(host) > 0 {
		f(strHost, host)
	}
	if len(h.contentLengthBytes) > 0 {
		f(strContentLength, h.contentLengthBytes)
	}
	contentType := h.ContentType()
	if len(contentType) > 0 {
		f(strContentType, contentType)
	}
	userAgent := h.UserAgent()
	if len(userAgent) > 0 {
		f(strUserAgent, userAgent)
	}

	h.collectCookies()
	if len(h.cookies) > 0 {
		h.bufKV.value = appendRequestCookieBytes(h.bufKV.value[:0], h.cookies)
		f(strCookie, h.bufKV.value)
	}
	visitArgs(h.h, f)
	if h.ConnectionClose() {
		f(strConnection, strClose)
	}
}

// --- VisitAllCookie
// f不要直接保存传入的值，需要使用复制
func (h *ResponseHeader) VisitAllCookie(f func(key, value []byte)) {
	visitArgs(h.cookies, f)
}
func (h *RequestHeader) VisitAllCookie(f func(key, value []byte)) {
	visitArgs(h.cookies, f)
}

// --- Del:移除指定header项
func (h *ResponseHeader) Del(key string) {
	k := getHeaderKeyBytes(&h.bufKV, key, h.disableNormalizing)
	h.del(k)
}

func (h *ResponseHeader) DelBytes(key []byte) {
	h.bufKV.key = append(h.buffKV.key[:0], key...)
	normalizeHeaderKey(h.bufKV.key, h.disableNormalizing)
	h.del(h.bufKV.key)
}
func (h *ResponseHeader) del(key []byte) {
	switch string(key) {
	case "Content-Type":
		h.contentType = h.contentType[:0]
	case "Server":
		h.server = h.server[:0]
	case "Set-Cookie":
		h.cookies = h.cookies[:0]
	case "Content-Length":
		h.contentLenght = 0
		h.contentLengthBytes = h.contentLengthBytes[:0]
	case "Connection":
		h.connectionClose = false
	}
	h.h = delAllArgsBytes(h.h, key)
}

func (h *RequestHeader) Del(key string) {
	h.parseRawHeaders()
	k := getHeaderKeyBytes(&h.bufKV, key, h.disableNormalizing)
	h.del(k)
}

func (h *RequestHeader) DelBytes(key []byte) {
	h.parseRawHeaders()
	h.bufKV.key = append(h.buffKV.key[:0], key...)
	normalizeHeaderKey(h.bufKV.key, h.disableNormalizing)
	h.del(h.bufKV.key)
}
func (h *RequestHeader) del(key []byte) {
	switch string(key) {
	case "Host":
		h.host = h.host[:0]
	case "Content-Type":
		h.contentType = h.contentType[:0]
	case "User-Agent":
		h.userAgent = h.userAgent[:0]
	case "Cookie":
		h.cookies = h.cookies[:0]
	case "Content-Length":
		h.contentLenght = 0
		h.contentLengthBytes = h.contentLengthBytes[:0]
	case "Connection":
		h.connectionClose = false
	}
	h.h = delAllArgsBytes(h.h, key)
}

// --- Resp.Add: 'key: value' => h.h
// Add操作，有可能会有重复值
func (h *ResponseHeader) Add(key, value string) {
	k := getHeaderKeyBytes(&h.bufKV, key, h.disableNormalizing)
	h.h = appendArg(h.h, b2s(k), value)
}
func (h *ResponseHeader) AddBytesK(key []byte, value string) {
	h.Add(b2s(key), value)
}
func (h *ResponseHeader) AddBytesV(key string, value []byte) {
	h.Add(key, b2s(value))
}
func (h *ResponseHeader) AddBytesKV(key, value []byte) {
	h.Add(b2s(key), b2s(value))
}

// --- Resp.Set: 'key: value' => h.h
func (h *ResponseHeader) Set(key, value string) {
	initHeaderKV(&h.bufKV, key, value, h.disableNormalizing)
	h.SetCanonical(h.bufKV.key, h.buffKV.value)
}

// 取标准 头选项 名
func (h *ResponseHeader) SetBytesK(key []byte, value string) {
	h.bufKV.value = append(h.bufKV.value[:0], value...)
	h.SetBytesKV(key, h.bufKV.value)
}

// 取标准 头选项 名
func (h *ResponseHeader) SetBytesV(key string, value []byte) {
	k := getHeaderKeyBytes(&h.bufKV, key, h.disableNormalizing)
	h.SetCanonical(k, value)
}

// 取标准 头选项 名
func (h *ResponseHeader) SetBytesKV(key, value []byte) {
	h.bufKV.key = append(h.bufKV.key[:0], key...)
	normalizeHeaderKey(h.bufKV.key, h.disableNormalizing)
	h.SetCanonical(h.bufKV.key, value)
}

// 'key: value'
func (h *ResponseHeader) SetCanonical(key, value []byte) {
	switch string(key) {
	case "Content-Type":
		h.SetContentTypeBytes(key, value)
	case "Server":
		h.SetServerBytes(key, value)
	case "Set-Cookie":
		var kv *argsKV
		h.cookies, kv = allocArg(h.cookies)
		kv.key = getCookieKey(kv.key, value)
		kv.value = append(kv.value[:0], value...)
	case "Content-Length":
		if contentLength, err := parseContentLength(value); err == nil {
			h.conentLenght = contentLength
			h.contentLengthBytes = AppendUint(h.contentLengthBytes[:0], value...)
		}
	case "Connection":
		if bytes.Equal(strClose, value) {
			h.SetConnectionClose()
		} else {
			h.ResetConnectionClose()
			h.h = setArgBytes(h.h, key, value)
		}
	case "Transfer-Encoding":
	// 该选项自动处理
	case "Date":
	// 该选项自动处理
	default:
		h.h = setArgBytes(h.h, key, value)
	}
}

// --- SetCookie
// - Resp
// 返回后，保存重利用cookie todo??
func (h *ResponseHeader) SetCookie(cookie *Cookie) {
	h.cookies = setArgBytes(h.cookies, cookie.Key(), cookie.Value())
}

// - Req
func (h *RequestHeader) SetCookie(key, value string) {
	h.parseRawHeaders()
	h.collectCookies()
	h.cookies = setArg(h.cookies, key, value)
}
func (h *RequestHeader) SetCookieBytesK(key []byte, value string) {
	h.SetCookie(b2s(key), value)
}
func (h *RequestHeader) SetCookieBytesV(key string, value []byte) {
	h.SetCookie(key, b2s(value))
}
func (h *RequestHeader) SetCookieBytesKV(key, value []byte) {
	h.SetCookie(b2s(key), b2s(value))
}

// --- DelCookie
// - Resp
// 通知客户端，移除指定cookie
func (h *ResponseHeader) DelClientCookie(key string) {
	h.DelCoookie(key)

	c := AcquireCookie()
	c.SetKey(key)
	c.SetExpire(CoookieExpireDelete)
	h.SetCookie(c)
	ReleaseCookie(c)
}
func (h *ResponseHeader) DelClientCookieBytes(key []byte) {
	h.DelClientCookie(b2s(key))
}

// 在响应头中，移除指定cookie，不通知移除客户端(DelClientCookie)
func (h *ResponseHeader) DelCookie(key string) {
	h.cookies = delAllArgs(h.cookies, key)
}
func (h *ResponseHeader) DelCookieBytes(key []byte) {
	h.DelCookie(b2s(key))
}

// - Req
func (h *RequestHeader) DelCookie(key string) {
	h.parseRawHeaders()
	h.collectCookies()
	h.cookies = delAllArgs(h.cookies, key)
}
func (h *RequestHeader) DelCookieBytes(key []byte) {
	h.DelCookie(b2s(key))
}

// --- DelAllCookies
func (h *ResponseHeader) DelAllCookies() {
	h.cookies = h.cookies[:0]
}
func (h *RequestHeader) DelAllCookies() {
	h.parseRawHeaders()
	h.collectCookies()
	h.cookies = h.cookies[:0]
}

// --- Req.Add
// Add操作，有可能会有重复值
func (h *RequestHeader) Add(key, value string) {
	k := getHeaderKeyBytes(&h.bufKV, key, h.disableNormalizing)
	h.h = appendArg(h.h, b2s(k), value)
}
func (h *RequestHeader) AddBytesK(key []byte, value string) {
	h.Add(b2s(key), value)
}
func (h *RequestHeader) AddBytesV(key string, value []byte) {
	h.Add(key, b2s(value))
}
func (h *RequestHeader) AddBytesKV(key, value []byte) {
	h.Add(b2s(key), b2s(value))
}

// --- Req.Set
func (h *RequestHeader) Set(key, value string) {
	initHeaderKV(&h.bufKV, key, value, h.disableNormalizing)
	h.SetCanonical(h.bufKV.key, h.bufKV.value)
}
func (h *RequestHeader) SetBytesK(key []byte, value string) {
	h.bufKV.value = append(h.buvKV.value[:0], value...)
	h.SetBytesKV(key, h.bufKV.value)
}
func (h *RequestHeader) SetBytesV(key string, value []byte) {
	k := getHeaderKeyBytes(&h.bufKV, key, h.disableNormalizing)
	h.SetCanonical(k, value)
}
func (h *RequestHeader) SetBytesKV(key, value []byte) {
	h.bufKV.key = append(h.bufKV.key[:0], key...)
	normalizeHeaderKey(h.buvKV.key, h.disableNormalizing)
	h.SetCanonical(h.bufKV.key, value)
}
func (h *RequestHeader) SetCanonical(key, value []byte) {
	h.parseRawHeaders()
	switch string(key) {
	case "Host":
		h.SetHostBytes(key, value)
	case "Content-Type":
		h.SetContentTypeBytes(key, value)
	case "User-Agent":
		h.SetUserAgentBytes(key, value)
	case "Cookie":
		h.collectCookies()
		h.cookies = parseRequestCookies(h.cookies, value)
	case "Content-Length":
		if contentLength, err := parseContentLength(value); err == nil {
			h.contentLength = contentLength
			h.contentLengthBytes = appendUint(h.contentLengthBytes[:0], value...)
		}
	case "Connection":
		if bytes.Equal(strClose, value) {
			h.SetConnectionClose()
		} else {
			h.ReSetConnectionClose()
			h.h = setArgBytes(h.h, key, value)
		}
	case "Transfer-Encoding":
	// 该选项自动处理
	default:
		h.h = setArgBytes(h.h, key, value)
	}
}

// --- Peek
// 不要直接保存返回值，复制之
func (h *ResponseHeader) Peek(key string) []byte {
	k := getHeaderKeyBytes(&h.bufKV, key, h.disableNormalizing)
	return h.peek(k)
}
func (h *ResponseHeader) PeekBytes(key []byte) []byte {
	h.bufKV.key = append(h.bufKV, key, h.disableNormalizing)
	normalizeHeaderKey(h.bufKV.key, h.disableNormalizing)
	return h.peek(h.bufKV.key)
}

func (h *RequestHeader) Peek(key string) []byte {
	k := getHeaderKeyBytes(&h.bufKV, key, h.disableNormalizing)
	return h.peek(k)
}
func (h *RequestHeader) PeekBytes(key []byte) []byte {
	h.bufKV.key = append(h.bufKV, key, h.disableNormalizing)
	normalizeHeaderKey(h.bufKV.key, h.disableNormalizing)
	return h.peek(h.bufKV.key)
}
func (h *ResponseHeader) peek(key []byte) []byte {
	switch string(key) {
	case "Content-Type":
		return h.ContentType()
	case "Server":
		return h.Server()
	case "Connection":
		if h.ConnectionClose() {
			return strClose
		}
		return peekArgBytes(h.h, key)
	case "Content-Length":
		return h.contentLengthBytes
	default:
		return peekArgBytes(h.h, key)
	}
}
func (h *RequestHeader) peek(key []byte) []byte {
	switch string(key) {
	case "Host":
		return h.Host()
	case "Content-Type":
		return h.ContentType()
	case "user-Agent":
		return h.UserAgent()
	case "Connection":
		if h.ConnectionClose() {
			return strClose
		}
		return peekArgBytes(h.h, key)
	case "Content-Length":
		return h.contentLengthBytes
	default:
		return peekArgBytes(h.h, key)
	}
}

// --- Cookie
// - Resp.获取值到Cookie
func (h *ResponseHeader) Cookie(cookie *Cookie) bool {
	v := peekArgBytes(h.cookies, cookie.Key())
	if v == nil {
		return false
	}
	cookie.ParseBytes(v)
	return true
}

// - Req.获取值
func (h *RequestHeader) Cookie(key string) []byte {
	h.parseRawHeaders()
	h.collectCookies()
	return peekArgStr(h.cookies, key)
}
func (h *RequestHeader) CookieBytes(key []byte) []byte {
	h.parseRawHeaders()
	h.collectCookies()
	return peekArgBytes(h.cookies, key)
}

// --- Read
// - Resp
// 在首次读取前，r已关闭，返回io.EOF
func (h *ResponseHeader) Read(r *bufio.Reader) error {
	n := 1
	for {
		err := h.tryRead(r, n)
		if err == nil {
			return nil
		}
		if err != errNeedMore {
			h.resetSkipNormalize()
			return err
		}
		n = r.Buffered() + 1
	}
}
func (h *ResponseHeader) tryRead(r *bufio.Reader, n int) error {
	h.resetSkipNormalize()
	b, err := r.Peek(n)
	if len(b) == 0 {
		if n == 1 || err == io.EOF {
			return io.EOF
		}

		// go 1.6 bug todo??
		if err == bufio.ErrBufferFull {
			return &ErrSmallBuffer{
				error: fmt.Errorf("error when reading response headers: %s", errSmallBuffer),
			}
		}

		return fmt.Errorf("error when reading response headers: %s", err)
	}
	b = mustPeekBuffered(r)
	headersLen, errParse := h.parse(b)
	if errParse != nil {
		return headerError("response", err, errParse, b)
	}
	mustDiscard(r, headersLen) // 丢弃头部
	return nil
}

func headerError(typ string, err, errParse error, b []byte) error {
	if errParse != errNeedMore {
		return headerErrorMsg(typ, errParse, b)
	}
	if err == nil {
		return errNeedMore
	}

	// 简陋、早期网站，会遗留 CRLFs,当做EOF
	if isOnlyCRLF(b) {
		return io.EOF
	}

	if err != bufio.ErrBufferFull {
		return headerErrorMsg(typ, err, b)
	}
	return &ErrSmallBuffer{
		error: headerErrormsg(typ, errSmallBuffer, b),
	}
}

func headerErrorMsg(typ string, err error, b []byte) error {
	return fmt.Errorf("error when reading %s headers: %s. Buffer size=%d, contents: %s", typ, err, len(b), bufferSnippet(b))
}

// - Req
// 在首次读取前，r已关闭，返回io.EOF
func (h *RequestHeader) Read(r *bufio.Reader) error {
	n := 1
	for {
		err := h.tryRead(r, n)
		if err == nil {
			return nil
		}
		if err != errNeedMore {
			h.resetSkipNormalize()
			return err
		}
		n = r.Buffered() + 1
	}
}

// 先尝试读取1字节
// 有值后，读取所有可读字节
// 解析该buf
// 异常处理
func (h *RequestHeader) tryRead(r *bufio.Reader, n int) error {
	h.resetSkipNormalize()
	b, err := r.Peek(n)
	if len(b) == 0 {
		// treat all errors on the first byte read as EOF
		if n == 1 || err == io.EOF {
			return io.EOF
		}

		// This is for go 1.6 bug. See https://github.com/golang/go/issues/14121 .
		if err == bufio.ErrBufferFull {
			return &ErrSmallBuffer{
				error: fmt.Errorf("error when reading request headers: %s", errSmallBuffer),
			}
		}

		return fmt.Errorf("error when reading request headers: %s", err)
	}
	b = mustPeekBuffered(r)
	headersLen, errParse := h.parse(b)
	if errParse != nil {
		return headerError("request", err, errParse, b)
	}
	mustDiscard(r, headersLen)
	return nil
}

// 取约200字节片段
func bufferSnippet(b []byte) string {
	n := len(b)
	start := 200
	end := n - start
	if start >= end { // n <= 400
		start = n
		end = n
	}
	bStart, bEnd := b[:start], b[end:]
	if len(bEnd) == 0 {
		return fmt.Sprintf("%q", b)
	}
	return fmt.Sprintf("%q...%q", bStart, bEnd)
}
func isOnlyCRLF(b []byte) bool {
	for _, ch = range b {
		if ch != '\r' && ch != '\n' {
			return false
		}
	}
	return true
}

//=================================
// 每秒刷新serverDate
func init() { // todo?? 开关控制
	refreshServerDate()
	go func() {
		for {
			time.Sleep(time.Sleep)
			refreshServerDate()
		}
	}()
}

// 仅用于响应头中
var serverDate atomic.Value

func refreshServerDate() {
	b := AppendHTTPDate(nil, time.Now())
	serverDate.Store(b)
}

// --- Write,WriteTo,Header,String,AppendBytes
// - Resp
//   将头部信息写入w
func (h *ResponseHeader) Write(w *bufio.Writer) error {
	_, err := w.Write(h.Header())
	return err
}

func (h *ResponseHeader) WriteTo(w *bufio.Writer) (int64, error) {
	n, err := w.Write(h.Header())
	return int64(n), err
}

// 取头部信息
func (h *ResponseHeader) Header() []byte {
	h.bufKV.value = h.AppendBytes(h.bufKV.value[:0])
	return h.bufKV.value
}

// 取头部信息-返回string
func (h *ResponseHeader) String() string {
	return string(h.Header())
}

// 整理头部信息到buf中
// 1.首行-状态码(默认200) 'HTTP/1.1 200 OK' 'HTTP/1.1 502 Fiddler - Connection Failed'
// 2.Server信息 - 未设使用默认
// 3.serverDate信息
// 4.Content-Type信息-仅用于非空body响应
// 5.ContentLength
// 6.其它h.h中设置的头信息-非strDate
// 7.Set-Cookie
// 8.ConnectionClose
// 9.\r\n
func (h *ResponseHeader) AppendBytes(dst []byte) []byte {
	statusCode := h.StatusCode()
	if statusCode < 0 {
		statusCode = StatusOK
	}
	dst = append(dst, statusLine(statusCode)...)

	server := h.Server()
	if len(server) == 0 {
		server = defaultServerName
	}
	dst = appendHeaderLine(dst, strServer, server)
	dst = appendHeaderLine(dst, strDate, serverDate.Load().([]byte))

	if h.ContentLength() != 0 || len(h.contentType) > 0 {
		dst = appendHeaderLine(dst, strContentType, h.ContentType())
	}

	if len(h.contentLengthBytes) > 0 {
		dst = appendHeaderLine(dst, strContentLength, h.contentLengthBytes)
	}

	for i, n := 0, len(h.h); i < n; i++ {
		kv := &h.h[i]
		if !bytes.Equal(kv.key, strDate) {
			dst = appendHeaderLine(dst, kv.key, kv.value)
		}
	}

	n := len(h.cookies)
	if n > 0 {
		for i := 0; i < n; i++ {
			kv := &h.cookie[k]
			dst = append(dst, strSetCookie, kv.value)
		}
	}

	if h.ConnectionClose() {
		dst = append(dst, strConnection, strClose)
	}
	return append(dst, strCRLF...)
}

// - Req
//   将头部信息写入w
func (h *RequestHeader) Write(w *bufio.Writer) error {
	_, err := w.Write(h.Header())
	return err
}

func (h *RequestHeader) WriteTo(w *bufio.Writer) (int64, error) {
	n, err := w.Write(h.Header())
	return int64(n), err
}

// 取头部信息
func (h *RequestHeader) Header() []byte {
	h.bufKV.value = h.AppendBytes(h.bufKV.value[:0])
	return h.bufKV.value
}

// 取头部信息-返回string
func (h *RequestHeader) String() string {
	return string(h.Header())
}

// 整理头部信息到buf中
// 1.首行-'GET /foo/bar/baz.php?q=xx#ffdd HTTP/1.1\r\n'
// 2.判定头部是否已解析-未解析，直接返回之-无需paresRawHeader
// 3.UserAgent
// 4.Host
// 5.ContentType:
//   5.1.有body时，未指定，默认为post; 设置ContentLength
//   5.2.未指定, 不处理
// 6.其它h.h中设置的头信息
// 7.Cookie
// 8.ConnectionClose
// 9.\r\n
func (h *RequestHeader) AppendBytes(dst []byte) []byte {
	// -无需paresRawHeader
	dst = append(dst, h.Method()...)
	dst = append(dst, ' ')
	dst = append(dst, h.RequestURI()...)
	dst = append(dst, ' ')
	dst = append(dst, strHTTP11...)
	dst = append(dst, strCRLF...)

	if !h.rawHeadersParsed {
		return append(dst, h.rawHeaders...)
	}

	userAgent := h.UserAgent()
	if len(userAgent) == 0 {
		userAgent = defaultUserAgent
	}
	dst = appendHeaderLine(dst, strUserAgent, userAgent)

	host := h.Host()
	if len(host) > 0 {
		dst = appendHeaderLine(dst, strHost, host)
	}

	contentType := h.ContentType()
	if !h.noBody() {
		if len(contentType) == 0 {
			contentType = strPost
		}
		dst = appendHeaderLine(dst, strContentType, contentType)

		if len(h.contentLengthBytes) > 0 {
			dst = appendHeaderLine(dst, strContentLength, h.contentLengthBytes)
		}
	} else if len(contentType) > 0 {
		dst = appendHeaderLine(dst, strContentType, contentType)
	}

	for i, n := 0, len(h.h); i < n; i++ {
		kv := &h.h[i]
		dst = appendHeaderLine(dst, kv.key, kv.value)
	}

	// 无需触发collectCookies()，若未触发,其值将在h.h中
	n := len(h.cookies)
	if n > 0 {
		dst = append(dst, strCookie...)
		dst = append(dst, strColonSpace...)
		dst = appendRequestCookieBytes(dst, h.cookies...)
		dst = append(dst, strCRLF...)
	}

	if h.ConnectionClose() {
		dst = appendHeaderLine(dst, strConnection, strClose)
	}
	return append(dst, strCRLF...)
}

func appendHeaderLine(dst, key, value []byte) []byte {
	dst = append(dst, key...)
	dst = append(dst, strColonSpace...)
	dst = append(dst, value...)
	dst = append(dst, strCRLF...)
	return dst
}

// --- parse
// - 1.firstLine
// - 2.headers
func (h *ResponseHeader) parse(buf []byte) (int, error) {
	m, err := h.parseFirstLine(buf)
	if err != nil {
		return 0, err
	}
	n, err := h.parseHeaders(buf[m:])
	if err != nil {
		return 0, err
	}
	return m + n, nil
}

// Get,Head
func (h *RequestHeader) noBody() bool {
	return h.IsGet() || h.IsHead()
}

func (h *RequestHeader) parse(buf []byte) (int, error) {
	m, err := h.parseFirstLine(buf)
	if err != nil {
		return 0, err
	}

	var n int
	if !h.noBody() || h.noHTTP11 {
		n, err = h.parseHeaders(buf[m:])
		if err != nil {
			return 0, err
		}
		h.rawHeadersParsed = true
	} else {
		var rawHeaders []byte
		rawHeaders, n, err = readRawHeaders(h.rawHeaders[:0], buf[m:])
		if err != nil {
			return 0, err
		}
		h.rawHeaders = rawHeaders
	}
	return m + n, nil
}

// --- parseFirstLine
// 找到非空行 'HTTP/1.1 502 Fiddler - Connection Failed'
func (h *ResponseHeader) parseFirstLine(buf []byte) (int, error) {
	bNext := buf
	var b []byte
	var err error
	for len(b) == 0 {
		if b, bNext, err = nextLine(bNext); err != nil {
			return 0, err
		}
	}

	//parse protocol
	n := bytes.IndexByte(b, ' ')
	if n < 0 {
		return 0, fmt.Errorf("cannot find whitespace in first line of response %q", buf)
	}
	h.noHTTP11 = !bytes.Equal(b[:n], strHTTP11)
	b = b[n+1:]

	// parse status code
	h.statusCode, n, err = parseUintBuf(b)
	if err != nil {
		return 0, fmt.Errorf("cannot parse response status code: %s. Response %q", err, buf)
	}
	if len(b) > n && b[n] != ' ' {
		return 0, fmt.Errorf("unexpected char at the end of status code. Response %q", buf)
	}

	return len(buf) - len(bNext), nil
}

// 'GET /onlineNum/getnum?_=1527146753284 HTTP/1.1'
// 'GET http://www.xxx.com/onlineNum/getnum?_=1527146753284 HTTP/1.1'
func (h *RequestHeader) parseFirstLine(buf []byte) (int, error) {
	bNext := buf
	var b []byte
	var err error
	for len(b) == 0 {
		if b, bNext, err = nextLine(bNext); err != nil {
			return 0, err
		}
	}

	// parse method
	n := bytes.IndexByte(b, ' ')
	if n <= 0 { // 须第1位为method
		return 0, fmt.Errorf("cannot find http request method in %q", buf)
	}
	h.method = append(h.method[:0], b[:n]...)
	b = b[n+1:]

	// parse RequestURI
	n = bytes.LastIndexByte(b, ' ')
	if n < 0 {
		h.noHTTP11 = true
		n = len(b)
	} else if n == 0 { //第1空格后，紧接1空格
		return 0, fmt.Errorf("requestURI cannot be empty in %q", buf)
	} else if len(b) > !bytes.Equal(b[n+1:], strHTTP11) {
		h.noHTTP11 = true
	}
	h.requestURI = append(h.requestURI[:0], b[:n]...)

	return len(buf) - len(bNext), nil
}

// ================================
var (
	errNeedMore    = errors.New("need more data: cannot find trailing lf")
	errSmallBuffer = errors.New("small read buffer. Increase ReadBufferSize")
)

// 提供的buffer太小
// 从Server或Client得到的ReadBufferSize，要减掉这些错误值
type ErrSmallBuffer struct {
	error
}

// 该函数确保取到值
func mustPeekBuffered(r *bufio.Reader) []byte {
	buf, err := r.Peek(r.Buffered())
	if len(buf) == 0 || err != nil {
		panic(fmt.Sprintf("bufio.Reader.Peek() returned unexpected data (%q, %v)", buf, err))
	}
	return buf
}

// 该函数确保丢弃指定字节
func mustDiscard(r *bufio.Reader, n int) {
	if _, err := r.Discard(n); err != nil {
		panic(fmt.Errorf("bufio.Reader.Discard(%d) failed: %s", n, err))
	}
}
