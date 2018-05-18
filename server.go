// selfFastHttp project selfFastHttp.go
package selfFastHttp

import (
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// 请求处理接口
// RequestHandler必须能处理请求
// 当返回后，要使用ctx内成员，须在返回前，调用ctx.TimeoutError()
// 当有响应时间限制，可将其封装在TimeoutHandler
type RequestHandler func(ctx *RequestCtx)

type Server struct {
	noCopy noCopy

	Handler RequestHandler //外部处理接口

	Name string // 服务器名,如果未设置，使用defaultServerName

	Concurrency int //一个server的并发数

	// 是否不使用长连接
	//
	// The server will close all the incoming connections after sending
	// the first response to client if this option is set to true.
	//
	// 默认允许长连接
	DisableKeepalive bool

	// 每个连接的读缓存区大小
	// 这个同样限制了header的大小
	// 如果用到大uris 或大headers(i.e. 大cookies).
	//
	// Default buffer size is used if not set.
	ReadBufferSize int

	// 每个连接的写缓存区大小
	// Default buffer size is used if not set.
	WriteBufferSize int

	// 读取1个请求数据(包括body)的等待时间
	//
	// 对闲置长连接生效 //todo
	// By default 无限制
	ReadTimeout time.Duration

	// 写操作超时时间(包括body)
	// 默认无限制
	WriteTimeout time.Duration

	// 针对ip限制并发最大连接数
	//
	// 默认无限制
	MaxConnsPerIP int

	// 每个连接的最大请求数(使用次数)
	//
	// 当最后一个请求结束，将关闭连接
	// 设置'Connection: close'头到最后一个响应里
	//
	// 默认无限制
	MaxRequestsPerConn int

	// 长连接的最大存活时间
	// 读或写触发 todo
	// 默认无限制
	MaxKeepaliveDuration time.Duration

	// 请求的最大body大小
	// server将拒绝超过大小的请求
	// 1.比对头部的contentLength,2.读取过程中检测
	// 未设置，使用默认值
	MaxRequestBodySize int

	// 通过高cpu占用方式，强制降低内存使用
	// 仅当server消耗太多内存在闲置长连接上，降低约50%以上内存
	// resetbody时，使用bufferpool方式,而非在连接上直接保留bodybuffer
	// 默认关闭
	ReduceMemoryUsage bool

	// 仅Get方式
	// 用于防ddos攻击，请求大小受ReadBufferSize限制
	// 默认允许所有方式 put delete get post head等
	GetOnly bool

	// 在生产环境中，会记录频繁的错误"connection reset by peer", "broken pipe", "connection timeout"
	// 默认不输出以上错误
	LogAllErrors bool

	// 启用后，header项将按原值传输
	// 仅当作为代理服务器，后续服务器对header中的各值敏感时，启用
	// 默认，不启用
	// cONTENT-lenGTH -> Content-Length
	DisableHeaderNamesNormalizing bool

	// Logger which is used by RequestCtx.Logger().
	// 默认使用log包
	Logger Logger

	concurrency      uint32           //当前并发数，有请求时，与Concurrency比对
	concurrencyCh    []chan struct{}  //限制并发数手段:能写入struct{}{}时，表示获得1个服务数,使用完取出下标志
	perIPConnCounter perIPConnCounter //每个ip的连接计数器
	serverName       atomic.Value     //实际使用的服务器名 响应时，填入

	ctxPool        sync.Pool //请求的上下文池
	readerPool     sync.Pool //请求的写缓存区池
	writerPool     sync.Pool //请求的读缓存区池
	hijackConnPool sync.Pool //被劫持的连接池
	bytePool       sync.Pool //字节分片池-用于读字节
}

//取得最大并发数
func (s *Server) getConcurrency() int {
	n := s.Concurrency
	if n <= 0 {
		n = DefaultConcurrency
	}
	return n
}

func (s *Server) writeFastError(w io.Writer, statusCode int, msg string) {

}

func acceptConn(s *Server, ln net.Listener, lastPerIPErrorTime *time.Time) (net.Conn, error) {
	return nil, nil
}

//取日志imp
func (s *Server) logger() Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return defaultLogger
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
