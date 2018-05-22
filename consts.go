package selfFastHttp

//存储全局常量

import (
	"errors"
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

	strGet  = []byte("GET")
	strHead = []byte("HEAD")
	strPost = []byte("POST")
	strPut  = []byte("PUT")
	strGet  = []byte("DELETE")

	strConnection      = []byte("Connection")
	strLocation        = []byte("Location")
	strIfModifiedSince = []byte("If-Modified-Since")
	strLastModified    = []byte("Last-Modified")
	strAcceptRanges    = []byte("Accept-Ranges")
	strRange           = []byte("Range")
	strContentRange    = []byte("Content-Range")

	strBytes = []byte("bytes")
)

var (
	errHijacked        = errors.New("connection has been hijacked")
	defaultServerName  = []byte("selfFastHttp")
	defaultContentType = []byte("text/plain; charset=utf-8")

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
