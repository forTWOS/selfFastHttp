package selfFastHttp

import (
	"io"
	"mime/multipart"

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

	// 聚集bool,减少Request对象大小??
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

func (*Response) Reset() {

}
