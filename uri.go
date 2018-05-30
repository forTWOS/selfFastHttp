package selfFastHttp

import (
	"bytes"
	"io"
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

	u.h = nil //todo ??
}

func (u *URI) CopyTo(dst *URI) {
	dst.Reset()

	dst.pathOriginal = append(dst.pathOriginal[:0], u.pathOriginal...)
	dst.scheme = append(dst.scheme[:0], u.scheme...)
	dst.path = append(dst.path[:0], u.path...)
	dst.queryString = append(dst.queryString[:0], u.queryString...)
	dst.fragment = append(dst.fragment[:0], u.fragment...)
	dst.host = append(dst.host[:0], u.host...)

	u.queryArgs.CopyTo(&dst.queryArgs)
	dst.parsedQueryArgs = u.parsedQueryArgs

	//	dst.fullURI = append(dst.fullURI[:0], u.fullURI...)
	//	dst.requestURI = append(dst.requestURI[:0], u.requestURI...)
	// 这个uri，用于处理头部URI，与其引用的Header为一套；耦合较深
	// todo??等全局时，再回头来确认该功能
	dst.h = u.h
}

// ----------------------------------
// i.e. qwe of http://xxx.com/foo/bar?baz=123&bap=456#qwe
func (u *URI) Fragment() []byte {
	return u.fragment
}

func (u *URI) SetFragment(fragment string) {
	u.fragment = append(u.fragment[:0], fragment...)
}

func (u *URI) SetFragmentBytes(fragment []byte) {
	u.fragment = append(u.fragment[:0], fragment...)
}

// i.e. baz=123&bap=456 of http://xxx.com/foo/bar?baz=123&bap=456#qwe
func (u *URI) QueryString() []byte {
	return u.queryString
}

func (u *URI) SetQueryString(queryString string) {
	u.queryString = append(u.queryString[:0], queryString...)
	u.parsedQueryArgs = false
}

func (u *URI) SetQueryStringBytes(queryString []byte) {
	u.queryString = append(u.queryString[:0], queryString...)
	u.parsedQueryArgs = false
}

// i.e. /foo/bar of http://xxx.com/foo/bar?baz=123&bap=456#qwe
// 返回值：解码+标准化
// i.e. '//f%20obar/baz/../zzz' becomes '/f obar/zzz'
func (u *URI) Path() []byte {
	if len(u.path) == 0 {
		return strSlash
	}
	return u.path
}

func (u *URI) SetPath(path string) {
	u.pathOriginal = append(u.pathOriginal[:0], path...)
	u.path = normalizePath(u.path, u.pathOriginal)
}

func (u *URI) SetPathBytes(path []byte) {
	u.pathOriginal = append(u.pathOriginal[:0], path...)
	u.path = normalizePath(u.path, u.pathOriginal)
}

// 返回path原始值
func (u *URI) PathOriginal() []byte {
	return u.pathOriginal
}

// i.e. http of http://xxx.com/foo/bar?baz=123&bap=456#qwe
// 返回值：小写化
func (u *URI) Scheme() []byte {
	if len(u.scheme) == 0 {
		return strHTTP
	}
	return u.scheme
}

func (u *URI) SetScheme(scheme string) {
	u.scheme = append(u.scheme[:0], scheme...)
	lowercaseBytes(u.scheme)
}

func (u *URI) SetSchemeBytes(scheme []byte) {
	u.scheme = append(u.scheme[:0], scheme...)
	lowercaseBytes(u.scheme)
}

// i.e xxx.com of http://xxx.com/foo/bar?baz=123&bap=456#qwe
// 返回值：最小化
// 从RequestHeader中，取得host数据
func (u *URI) Host() []byte {
	if len(u.host) == 0 && u.h != nil {
		u.host = append(u.host[:0], u.h.Host()...)
		lowercaseBytes(u.host)
		u.h = nil // todo??
	}
	return u.host
}

func (u *URI) SetHost(host string) {
	u.host = append(u.host[:0], host...)
	lowercaseBytes(u.host)
}

func (u *URI) SetHostBytes(host []byte) {
	u.host = append(u.host[:0], host...)
	lowercaseBytes(u.host)
}

// 初化化
// host可nil,当此时,uri中包含完整信息
// 当host不为空,uri可仅包含RequestURI
// .i.e. host: http://xxx.com
// .i.e. uri: /foo/bar?baz=123&bap=456#qwe
func (u *URI) Parse(host, uri []byte) {
	u.parse(host, uri, nil)
}

func (u *URI) parseQuick(uri []byte, h *RequestHeader, isTLS bool) {
	u.parse(nil, uri, h)
	if isTLS {
		u.scheme = append(u.scheme[:0], strHTTPS...)
	}
}

// 初化化
// host可nil,当此时,uri中包含完整信息
// 当host不为空,uri可仅包含RequestURI
// .i.e. host: http://xxx.com
// .i.e. uri: /foo/bar?baz=123&bap=456#qwe
func (u *URI) parse(host, uri []byte, h *RequestHeader) {
	u.Reset()
	u.h = h //todo?? new and CopyTo

	// 初始化scheme,host, uri
	scheme, host, uri := splitHostURI(host, uri) //返回值,有可能均从uri引用
	u.scheme = append(u.scheme, scheme...)
	lowercaseBytes(u.scheme)
	u.host = append(u.host, host...)
	lowercaseBytes(u.host)

	if len(uri) == 0 || (len(uri) == 1 && uri[0] == '/') { //uri为空(首页) 或 '/'
		u.path = append(u.path, '/')
		return
	}

	b := uri
	queryIndex := bytes.IndexByte(b, '?')
	fragmentIndex := bytes.IndexByte(b, '#')
	// 忽略fragment中的?
	if fragmentIndex >= 0 && queryIndex > fragmentIndex {
		queryIndex = -1
	}

	// 没有query、fragment,即全是path
	if queryIndex < 0 && fragmentIndex < 0 {
		u.pathOriginal = append(u.pathOriginal, b...)
		u.path = normalizePath(u.path, u.pathOriginal)
		return
	}

	if queryIndex >= 0 {
		//path肯定在最前
		u.pathOriginal = append(u.pathOriginal, b[:queryIndex]...)
		u.path = normalizePath(u.path, u.pathOriginal)

		if fragmentIndex < 0 {
			u.queryString = append(u.queryString, b[queryIndex+1:]...)
		} else {
			u.queryString = append(u.queryString, b[queryIndex+1:fragmentIndex]...)
			u.fragment = append(u.fragment, b[fragmentIndex:]...)
		}
		return
	}

	// queryIndex < 0 && fragmentIndex >= 0
	// path肯定在最前
	u.pathOriginal = append(u.pathOriginal, b[:fragmentIndex]...)
	u.path = normalizePath(u.path, u.pathOriginal)
	u.fragment = append(u.fragment, b[fragmentIndex+1:]...)
}

// +优化
// 简单解析uri-it's useful for http/https
// 从传入的host,uri 初始化 scheme(默认http), host, uri
// 1.在uri中查找'://'--确认host,scheme
//    1.1.未找到,scheme=srcHTTP,并返回
// 2.检测从uri中获得的scheme合法性 -- 未找到,同1.1处理
// 3.在后续字串中，查找'/'
//   3.1.未找到，查找? i.e. xxx.com?act=index&mod=admin
//   3.2.确认为host-粗略
// ps: scheme includes http, file, git, ftp, ed2k...
func splitHostURI(host, uri []byte) ([]byte, []byte, []byte) {
	//------scheme
	n := bytes.Index(uri, strSlashSlash)
	if n < 0 { // '//query?str=1#fqe'
		//未找到scheme,则无法从uri确定host
		return strHTTP, host, uri
	}
	scheme := uri[:n]
	if n == 0 { //默认http
		scheme = strHTTP
	}

	//检测scheme合法性
	if bytes.IndexByte(scheme, '/') >= 0 {
		return strHTTP, host, uri
	}
	//--------------

	n += len(strSlashSlash)
	if len(uri) == n { //url empty
		return strHTTP, host, strSlash
	}
	uri = uri[n:]

	// 3.在后续字串中，查找'/'
	n = bytes.IndexByte(uri, '/')
	if n < 0 {
		// i.e. xxx.com?act=index&mod=admin
		if n = bytes.IndexByte(uri, '?'); n >= 0 {
			return scheme, uri[:n], uri[n:]
		}
		return scheme, uri, strSlash
	}
	return scheme, uri[:n], uri[n:]
}

// 标准化path
// 0.初始化dst
// 1.最前加'/'
// 2.解码%xx
// 3.循环-处理多重'/'
// 4.移除'/./'
// 5.移除'/foo/../'
// 6.移除'/foo/..'尾部
func normalizePath(dst, src []byte) []byte {
	dst = dst[:0]                         //0.初始化dst
	dst = addLeadingSlash(dst, src)       //1.最前加'/'
	dst = decodeArgAppendNoPlus(dst, src) //2.解码%xx

	// 3.循环-处理多重'/'
	b := dst //游标
	bSize := len(b)
	for {
		n := bytes.Index(b, strSlashSlash)
		if n < 0 {
			break
		}
		b = b[n:]
		copy(b, b[1:])   //前移1位
		b = b[:len(b)-1] //截取
		bSize--
	}
	dst = dst[:bSize]

	// 4.移除'/./'
	b = dst
	for {
		n := bytes.Index(b, strSlashDotSlash)
		if n < 0 {
			break
		}
		nn := n + len(strSlashDotSlash) - 1 //防止不同编码异常
		copy(b[n:], b[nn:])
		b = b[:len(b)-nn+n]
	}

	// 5.移除'/foo/../'
	for {
		n := bytes.Index(b, strSlashDotDotSlash)
		if n < 0 {
			break
		}
		nn := bytes.LastIndexByte(b[:n], '/') // 找到前一个'/'
		if nn < 0 {// 未找到，表示根目录
			nn = 0
		}
		n += len(strSlashDotDotSlash) - 1 //防止不同编码异常
		copy(b[nn:], b[n:])
		b = b[:len(b)+nn-n]
	}

	// 6.移除尾部'/foo/..'
	n := bytes.LastIndex(b, strSlashDotDot)
	if n >= 0 && n+len(strSlashDotDot) == len(b) {
		nn := bytes.LastIndexByte(b[:n], '/')
		if nn < 0 {
			return strSlash
		}
		b = b[:nn+1]
	}

	return b
}

// 重组RequestURI，并传出
// i.e. 仅URI部份，不含scheme,host
// path + queryArgs/queryString + fragment
func (u *URI) RequestURI() []byte {
	dst := appendQuotedPath(u.requestURI[:0], u.Path())
	if u.queryArgs.Len() > 0 {
		dst = append(dst, '?')
		dst = u.queryArgs.AppendBytes(dst)
	} else if len(u.queryString) > 0 {
		dst = append(dst, '?')
		dst = append(dst, u.queryString...)
	}
	if len(u.fragment) > 0 {
		dst = append(dst, '#')
		dst = append(dst, u.fragment...)
	}
	u.requestURI = dst
	return u.requestURI
}

// LastPathSegment: 返回最后一个'/'之后的内容
//   * /foo/bar/baz.html => baz.html
//   * /foo/bar/ =>
//   * /foobar.js => foobar.js
//   * foobar.js => foobar.js
func (u *URI) LastPathSegment() []byte {
	path := u.Path()
	n := bytes.LastIndexByte(path, '/')
	if n < 0 {
		return path
	}
	return path[n+1:]
}

// 根据传入的newURI,更新相应值:scheme, host, uri, path/pathOriginal, fragment
// 接受以下格式:
//  * 完整格式 http://xxx.com/aa/bb?cc => 替换original uri
//  * 不含scheme, //xxx.com/aa/bb?cc => scheme使用默认值
//  * 不含http, /aa/bb?cc => 仅替换RequestURI
//  * 相对路径, bb?cc => 根据相对路径，更新RequestURI
// 然后parse
func (u *URI) Update(newURI string) {
	u.UpdateBytes(s2b(newURI))
}

func (u *URI) UpdateBytes(newURI []byte) {
	u.requestURI = u.updateBytes(newURI, u.requestURI)
}

func (u *URI) updateBytes(newURI, buf []byte) []byte {
	if len(newURI) == 0 {
		return buf
	}

	n := bytes.Index(newURI, strSlashSlash)
	if n >= 0 {
		//取出原scheme，若有值，而传入newURI不含scheme，则用之
		var b [32]byte
		schemeOriginal := b[:0]
		if len(u.scheme) > 0 {
			schemeOriginal = append([]byte(nil), u.scheme...) //新建并复制一份scheme
		}
		u.Parse(nil, newURI)
		if len(schemeOriginal) > 0 && len(u.scheme) == 0 {
			u.scheme = append(u.scheme[:0], schemeOriginal...)
		}
		return buf
	}

	// i.e. /foo/bar?cc=1
	if newURI[0] == '/' {
		buf = u.appendSchemeHost(buf[:0])
		buf = append(buf, newURI...)
		u.Parse(nil, buf)
		return buf
	}

	// xx/aa?bb=1
	switch newURI[0] {
	case '?': //仅传入querystring
		u.SetQueryStringBytes(newURI[1:])
		return append(buf[:0], u.FullURI()...)
	case '#': //仅传入fragment
		u.SetFragmentBytes(newURI[1:])
		return append(buf[:0], u.FullURI()...)
	default:
		path := u.Path()
		n = bytes.LastIndexByte(path, '/')
		if n < 0 {
			panic("BUG: path must contain at least one slash")
		}
		buf = u.appendSchemeHost(buf[:0])
		buf = appendQuotedPath(buf, path[:n+1])
		buf = append(buf, newURI...)
		u.Parse(nil, buf)
		return buf
	}
}

// 返回: {Scheme}://{Host}{RequestURI}#{Fragment}
func (u *URI) FullURI() []byte {
	u.fullURI = u.AppendBytes(u.fullURI[:0])
	return u.fullURI
}

func (u *URI) AppendBytes(dst []byte) []byte {
	dst = u.appendSchemeHost(dst)
	return append(dst, u.RequestURI()...)
}

// 将scheme://host附到dst
func (u *URI) appendSchemeHost(dst []byte) []byte {
	dst = append(dst, u.Scheme()...)
	dst = append(dst, strColonSlashSlash...)
	return append(dst, u.Host()...)
}

// 将full uri写入io.Writer
// w是io写入型接口
func (u *URI) WriteTo(w io.Writer) (int64, error) {
	n, err := w.Write(u.FullURI())
	return int64(n), err
}

// 字串化
func (u *URI) String() string {
	return string(u.FullURI())
}

func (u *URI) QueryArgs() *Args {
	u.parseQueryArgs()
	return &u.queryArgs
}

// 格式化queryArgs <= queryString
func (u *URI) parseQueryArgs() {
	if u.parsedQueryArgs {
		return
	}
	u.queryArgs.ParseBytes(u.queryString)
	u.parsedQueryArgs = true
}
