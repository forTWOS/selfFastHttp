package selfFastHttp

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"time"
)

var zeroTime time.Time

var (
	// 设置一个过去的时间，让其过期被移除
	CookieExpireDelete = time.Date(2009, time.November, 10, 23, 0, 0, 0, time.UTC)

	// 不限时,默认
	CookieExpireUnlimited = zeroTime
)

func AcquireCookie() *Cookie {
	return cookiePool.Get().(*Cookie)
}

func ReleaseCookie(c *Cookie) {
	c.Reset()
	cookiePool.Put(c)
}

var cookiePool = &sync.Pool{
	New: func() interface{} {
		return &Cookie{}
	},
}

// 用于响应cookie相关处理
// 不要直接保存，创建并CopyTo
// 不可用于并发处理
type Cookie struct {
	noCopy noCopy

	key    []byte
	value  []byte
	expire time.Time
	domain []byte
	path   []byte

	httpOnly bool
	secure   bool

	bufKV argsKV // 工具串
	buf   []byte //输入串
}

// ================================
func (c *Cookie) CopyTo(dst *Cookie) {
	//dst.Reset() // not need
	dst.key = append(dst.key[:0], c.key...)
	dst.value = append(dst.value[:0], c.value...)
	dst.expire = c.expire
	dst.domain = append(dst.domain[:0], c.domain...)
	dst.path = append(dst.path[:0], c.path...)

	dst.httpOnly = c.httpOnly
	dst.secure = c.secure
}

// - hostOnly
func (c *Cookie) HTTPOnly() bool {
	return c.httpOnly
}

func (c *Cookie) SetHTTPOnly(httpOnly bool) {
	c.httpOnly = httpOnly
}

// - secure
func (c *Cookie) Secure() bool {
	return c.secure
}

func (c *Cookie) SetSecure(secure bool) {
	c.secure = secure
}

// - path
func (c *Cookie) Path() []byte {
	return c.path
}

func (c *Cookie) SetPath(path string) {
	c.buf = append(c.buf[:0], path...)
	c.path = normalizePath(c.path, c.buf)
}

func (c *Cookie) SetPathBytes(path []byte) {
	c.buf = append(c.buf[:0], path...)
	c.path = normalizePath(c.path, c.buf)
}

// - domain
func (c *Cookie) Domain() []byte {
	return c.domain
}

func (c *Cookie) SetDomain(domain string) {
	c.domain = append(c.domain[:0], domain...)
}

func (c *Cookie) SetDomainBytes(domain []byte) {
	c.domain = append(c.domain[:0], domain...)
}

// - expire
func (c *Cookie) Expire() time.Time {
	expire := c.expire
	if expire.IsZero() {
		expire = CookieExpireUnlimited
	}
	return expire
}

func (c *Cookie) SetExpire(expire time.Time) {
	c.expire = expire
}

// - value
func (c *Cookie) Value() []byte {
	return c.value
}
func (c *Cookie) SetValue(value string) {
	c.value = append(c.value[:0], value...)
}
func (c *Cookie) SetValueBytes(value []byte) {
	c.value = append(c.value[:0], value...)
}

// - key
func (c *Cookie) Key() []byte {
	return c.key
}
func (c *Cookie) SetKey(key string) {
	c.key = append(c.key[:0], key...)
}
func (c *Cookie) SetKeyBytes(key []byte) {
	c.key = append(c.key[:0], key...)
}

func (c *Cookie) Reset() {
	c.key = c.key[:0]
	c.value = c.value[:0]
	c.expire = zeroTime
	c.domain = c.domain[:0]
	c.path = c.path[:0]
	c.httpOnly = false
	c.secure = false
}

// 用于Response
// Set-Cookie: user_info=currentNewsGuid22248=9d565bae-2f9b-449d-9456-dc71c1582b9b; expires=Sun, 24-Jun-2018 09:53:08 GMT; path=/
// key     : user_info
// value   : currentNewsGuid22248=9d565bae-2f9b-449d-9456-dc71c1582b9b
// expires : Sun, 24-Jun-2018 09:53:08 GMT
// path    : /
func (c *Cookie) AppendBytes(dst []byte) []byte {
	if len(c.key) > 0 {
		dst = append(dst, c.key...)
		dst = append(dst, '=')
	}
	dst = append(dst, c.value...)

	if !c.expire.IsZero() {
		c.bufKV.value = AppendHTTPDate(c.bufKV.value[:0], c.expire)
		dst = append(dst, ';', ' ')
		dst = append(dst, strCookieExpires...)
		dst = append(dst, '=')
		dst = append(dst, c.bufKV.value...)
	}
	if len(c.domain) > 0 {
		dst = appendCookiePart(dst, strCookieDomain, c.domain)
	}
	if len(c.path) > 0 {
		dst = appendCookiePart(dst, strCookiePath, c.path)
	}
	if c.httpOnly {
		dst = append(dst, ';', ' ')
		dst = append(dst, strCookieHTTPOnly...)
	}
	if c.secure {
		dst = append(dst, ';', ' ')
		dst = append(dst, strCookieSecure...)
	}
	return dst
}

func (c *Cookie) Cookie() []byte {
	c.buf = c.AppendBytes(c.buf[:0])
	return c.buf
}
func (c *Cookie) String() string {
	return string(c.Cookie())
}

// 实现 io.WriteTo 接口
func (c *Cookie) WriteTo(w io.Writer) (int64, error) {
	n, err := w.Write(c.Cookie())
	return int64(n), err
}

var errNoCookies = errors.New("no cookies found")

// 解析 'Set-Cookie: xxx'头
func (c *Cookie) Parse(src string) error {
	c.buf = append(c.buf[:0], src...)
	return c.ParseBytes(c.buf)
}

// 解析 'Set-Cookie: xxx'头
func (c *Cookie) ParseBytes(src []byte) error {
	c.Reset()

	var s cookieScanner
	s.b = src

	kv := &c.bufKV
	if !s.next(kv) {
		return errNoCookies
	}

	c.key = append(c.key[:0], kv.key...)
	c.value = append(c.value[:0], kv.value...)

	for s.next(kv) {
		if len(kv.key) == 0 && len(kv.value) == 0 {
			continue
		}
		switch string(kv.key) {
		case "expires":
			v := b2s(kv.value)
			exptime, err := time.ParseInLocation(time.RFC1123, v, time.UTC)
			if err != nil {
				return err
			}
			c.expire = exptime
		case "domain":
			c.domain = append(c.domain[:0], kv.value...)
		case "path":
			c.path = append(c.path[:0], kv.value...)
		case "":
			switch string(kv.value) {
			case "HttpOnly":
				c.httpOnly = true
			case "secure":
				c.secure = true
			}
		}
	}
	return nil
}

func appendCookiePart(dst, key, value []byte) []byte {
	dst = append(dst, ';', ' ')
	dst = append(dst, key...)
	dst = append(dst, '=')
	return append(dst, value...)
}

// get key from xx in 'Set-Cookie: xx'
func getCookieKey(dst, src []byte) []byte {
	n := bytes.IndexByte(src, '=')
	if n >= 0 {
		src = src[:n]
	}
	return decodeCookieArg(dst, src, false)
}

// 用于Request的appendCookieBytes
// Cookie: UM_distinctid=1623edb93670-0c6fb35140f2ca-5b123112-232800-1623edb93684bd; __utmc=1
// key1  : UM_distinctid
// value1: 1623edb93670-0c6fb35140f2ca-5b123112-232800-1623edb93684bd
// key2  : __utmc
// value2: 1
func appendRequestCookieBytes(dst []byte, cookies []argsKV) []byte {
	for i, n := 0, len(cookies); i < n; i++ {
		kv := &cookies[i]
		if len(kv.key) > 0 {
			dst = append(dst, kv.key...)
			dst = append(dst, '=')
		}
		dst = append(dst, kv.value...)
		if i+1 < n {
			dst = append(dst, ';', ' ')
		}
	}
	return dst
}

// 用于Request中的Cookie: 仅键值对,以'; '分隔
func parseRequestCookies(cookies []argsKV, src []byte) []argsKV {
	var s cookieScanner
	s.b = src
	var kv *argsKV
	cookies, kv = allocArg(cookies)
	for s.next(kv) {
		if len(kv.key) > 0 || len(kv.value) > 0 {
			cookies, kv = allocArg(cookies)
		}
	}
	return releaseArg(cookies)
}

type cookieScanner struct {
	b []byte
}

// Set-Cookie: user_info=currentNewsGuid22248=9d565bae-2f9b-449d-9456-dc71c1582b9b; expires=Sun, 24-Jun-2018 09:53:08 GMT; path=/
func (s *cookieScanner) next(kv *argsKV) bool {
	b := s.b
	if len(b) == 0 {
		return false
	}

	isKey := true
	k := 0                // flag for search
	for i, c := range b { // 一次遍历，即可解析
		switch c {
		case '=':
			if isKey {
				isKey = false
				kv.key = decodeCookieArg(kv.key, b[:i], false)
				k = i + 1
			}
		case ';':
			if isKey { // not found '=' i.e. HttpOnly, secure
				kv.key = kv.key[:0]
			}
			kv.value = decodeCookieArg(kv.value, b[k:i], true)
			s.b = b[i+1:]
			return true
		}
	}

	if isKey {
		kv.key = kv.key[:0]
	}
	kv.value = decodeCookieArg(kv.value, b[k:], true)
	s.b = b[len(b):] // 置空s.b
	return true
}

// 格式化src,放入dst
// 格式化:清除头尾空格，按条件清除头尾 双引号
func decodeCookieArg(dst, src []byte, skipQuotes bool) []byte {
	for len(src) > 0 && src[0] == ' ' {
		src = src[1:]
	}
	for len(src) > 0 && src[len(src)-1] == ' ' {
		src = src[:len(src)-1]
	}
	if skipQuotes {
		if len(src) > 1 && src[0] == '"' && src[len(src)-1] == '"' {
			src = src[1 : len(src)-1]
		}
	}
	return append(dst[:0], src...)
}
