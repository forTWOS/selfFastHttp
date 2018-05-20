package selfFastHttp

import (
	"sync"
)

// URI工厂
// 使用完，用ReleaseURI回收
func AcquireURI() *URI {
	return uriPool.Get().(*URI)
}

func ReleaseURI(u *URI) {
	u.Reset()
	uriPool.Put(u)
}

var uriPool = &sync.Pool{
	New: func() interface{} {
		return &URI{}
	},
}

// 统一资源标志符URI就是在某一规则下能把一个资源独一无二地标识出来
// URL在于Locater，一般来说（URL）统一资源定位符，可以提供找到该资源的路径
// uri组成:
//   访问资源的命名机制。
//   存放资源的主机名。
//   资源自身的名称，由路径表示。
type URI struct {
	noCopy noCopy

	pathOriginal []byte // 原始path
	scheme       []byte // 请求协议 http://
	path         []byte // 整理后的path
	queryString  []byte // ?之后的字串
	fragment     []byte // 片段,url中的#之后字串
	host         []byte // 主机地址

	queryArgs       Args // 整理后的query
	parsedQueryArgs bool // 是否已解析query

	fullURI    []byte // 完整url 由FullURI()在调用时生成
	requestURI []byte // 请求url 由RequestURI()在调用时生成

	h *RequestHeader
}

func (u *URI) Reset() {
	u.pathOriginal = u.pathOriginal[:0]
	u.scheme = u.scheme[:0]
	u.path = u.path[:0]
	u.queryString = u.queryString[:0]
	u.fragment = u.fragment[:0]
	u.host = u.host[:0]
	u.queryArgs.Reset()
	u.parsedQueryArgs = false

	//	u.fullURI = u.fullURI[:0]
	//	u.requestURI = u.requestURI[:0]

	u.h = nil
}
func (u *URI) CopyTo(dst *URI) {
	dst.Reset()

	dst.pathOriginal = append(dst.pathOriginal[:0], u.pathOriginal...)
	dst.scheme = append(dst.scheme[:0], u.scheme...)
	dst.path = append(dst.path[:0], u.path...)
	dst.queryString = append(dst.queryString[:0], u.queryString...)
	dst.fragment = append(dst.fragment[:0], u.fragment...)
	dst.host = append(dst.host[:0], u.host...)

	u.queryArgs.CopyTo(dst.queryArgs)
	dst.parsedQueryArgs = u.parsedQueryArgs

	//	dst.fullURI = append(dst.fullURI[:0], u.fullURI...)
	//	dst.requestURI = append(dst.requestURI[:0], u.requestURI...)

	if u.h != nil {
		if dst.h == nil {
			dst.h = &RequestHeader{}
		}
		u.h.CopyTo(dst.h)
	} else {
		if dst.h != nil {
			dst.h.Reset()
		}
	}
}
