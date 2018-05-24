package selfFastHttp

//存储全局常量

import (
	"errors"
)

var (
	defaultServerName  = []byte("selfFastHttp")
	defaultUserAgent   = []byte("selfFastHttp")
	defaultContentType = []byte("text/plain; charset=utf-8")
)

var (
	strSlash            = []byte("/")
	strSlashSlash       = []byte("//")
	strSlashDotDot      = []byte("/..")
	strSlashDotSlash    = []byte("/./")
	strSlashDotDotSlash = []byte("/../")
	strCRLF             = []byte("\r\n")
	strHTTP             = []byte("http")
	strHTTPS            = []byte("https")
	strHTTP11           = []byte("HTTP/1.1")
	strColonSlashSlash  = []byte("://")
	strColonSpace       = []byte(": ")
	strGMT              = []byte("GMT")

	strResponseContinue = []byte("HTTP/1.1 100 Continue\r\n\r\n")

	strGet    = []byte("GET")
	strHead   = []byte("HEAD")
	strPost   = []byte("POST")
	strPut    = []byte("PUT")
	strDelete = []byte("DELETE")

	// i.e. 'xx: gzip'
	strExpect           = []byte("Expect") // 'Expect: 100-continue'，遇到不支持HTTP/1.1的代理或服务器，会返回417错误
	strConnection       = []byte("Connection")
	strContentLength    = []byte("Content-Length")
	strContentType      = []byte("Content-Type")
	strDate             = []byte("Date")
	strHost             = []byte("Host")
	strReferer          = []byte("Referer") // 上一页url
	strServer           = []byte("Server")
	strTransferEncoding = []byte("Transfer-Encoding") // 机制：不依赖头部的长度信息，也能知道实体的边界——分块编码（Transfer-Encoding: chunked）
	strContentEncoding  = []byte("Content-Encoding")
	strAcceptEncoding   = []byte("Accept-Encoding")
	strUserAgent        = []byte("User-Agent")
	strCookie           = []byte("Cookie")     // 客户端请求头
	strSetCookie        = []byte("Set-Cookie") //服务端响应头
	strLocation         = []byte("Location")
	strIfModifiedSince  = []byte("If-Modified-Since")
	strLastModified     = []byte("Last-Modified")
	strAcceptRanges     = []byte("Accept-Ranges")
	strRange            = []byte("Range")
	strContentRange     = []byte("Content-Range")

	// Cookie
	strCookieExpires  = []byte("expires")
	strCookieDomain   = []byte("domain")
	strCookiePath     = []byte("path")
	strCookieHTTPOnly = []byte("HttpOnly") // 使cookie在浏览器中不可见-js、applet不可获得,防xss攻击
	strCookieSecure   = []byte("secure")   // 仅在https下传输

	// 'Connection: xx'
	strClose               = []byte("close")
	strGzip                = []byte("gzip")
	strDeflate             = []byte("deflate") //压缩
	strKeepAlive           = []byte("keep-alive")
	strKeepAliveCamelCase  = []byte("Keep-Alive")
	strUpgrade             = []byte("Upgrade")
	strChunked             = []byte("chunked")  // 'Transfer-Encoding: chunked'
	strIdentity            = []byte("identity") //HTTP/1.1弃用 'Transfer-Encoding: identity'
	str100Continue         = []byte("100-continue")
	strPostArgsContentType = []byte("application/x-www-form-urlencoded")
	strMultipartFormData   = []byte("multipart/form-data")
	strBoundary            = []byte("boundary") // multipart/form-data的分隔符
	strBytes               = []byte("bytes")
	strTextSlash           = []byte("text/")
	strApplicationSlash    = []byte("application/")
)

var (
	errHijacked = errors.New("connection has been hijacked")

	ErrNoArgValue = errors.New("no Arg value for the given key")
)

const (
	DefaultConcurrency     = 256 * 1024
	defaultReadBufferSize  = 4 * 1024 //原生go的http，也是这个值，看需求，360服务器改为1k
	defaultWriteBufferSize = 4 * 1024

	// DefaultMaxRequestBodySize is the maximum request body size the server
	// reads by default.
	//
	// See Server.MaxRequestBodySize for details.
	DefaultMaxRequestBodySize = 4 * 1024 * 1024
)
