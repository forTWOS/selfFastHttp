package selfFastHttp

//存储全局常量

import (
	"errors"
)

var (
	errHijacked       = errors.New("connection has been hijacked")
	defaultServerName = []byte("selfFastHttp")
	ErrNoArgValue     = errors.New("no Arg value for the given key")
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
