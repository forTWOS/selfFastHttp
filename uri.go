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
	hash         []byte // 片段,url中的#之后字串
	host         []byte // 主机地址

	queryArgs       Args // 整理后的query
	parsedQueryArgs bool // 是否已解析query

	fullURI    []byte // 完整url 由FullURI()在调用时生成
	requestURI []byte // 请求url 由RequestURI()在调用时生成

	h *RequestHeader
}
