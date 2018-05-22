package selfFastHttp

import "time"

// 禁止直接复制值,正确做法:新建并CopyTo
// 不可用于并发
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

	cookies []argsKV // 与cookiesCollected合用
}

//=========================================
// 'Content-Range: bytes startPos-endPos/contentLength'
// 用于响应分段请求数据
func (h *ResponseHeader) SetContentRange(startPos, endPos, contentLength int) {
	b := h.bufKV.value[:0]
	b = append(b, strBytes...)
	b = append(b, ' ')
	b = append(b, startPos)
	b = append(b, '-')
	b = append(b, endPos)
	b = append(b, '/')
	b = append(b, contentLength)
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
		b = appendUint(b, startPos)
	} else {
		endPos = -startPos
	}
	b = append(b, '-')
	if endPos >= 0 {
		b = appendUint(b, endPos)
	}
	h.bufKV.value = b
	h.SetCanonical(strRange, h.bufKV.value)
}

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
