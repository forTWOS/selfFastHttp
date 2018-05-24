package selfFastHttp

import (
	"bytes"
	"io"
	"sync"
)

// Args工厂
func AcquireArgs() *Args {
	return argsPool.Get().(*Args)
}

// 释放Args
func ReleaseArgs(a *Args) {
	a.Reset()
	argsPool.Put(a)
}

// Args池
var argsPool = &sync.Pool{
	New: func() interface{} {
		return &Args{}
	},
}

// 表示query参数
// 不支持并发使用
// 不可直接复制
type Args struct {
	noCopy noCopy

	args []argsKV // 解析后的值
	buf  []byte   // 原始输入字串 || 重组字串
}

// 键值对
type argsKV struct {
	key   []byte
	value []byte
}

// 重置
func (a *Args) Reset() {
	a.args = a.args[:0]
	a.buf = a.buf[:0]

}

// 复制到
func (a *Args) CopyTo(dst *Args) {
	dst.Reset()
	dst.args = copyArgs(dst.args, a.args)
}

// 在f中，不可直接引用值
// 如有需要，复制一份
func (a *Args) VisitAll(f func(key, value []byte)) {
	visitArgs(a.args, f)
}

func (a *Args) Len() int {
	return len(a.args)
}

// 解析传入字串
func (a *Args) Parse(s string) {
	a.buf = append(a.buf[:0], s...)
	a.ParseBytes(a.buf)
}

// 1.清空数据
// 2.使用解析器处理
func (a *Args) ParseBytes(b []byte) {
	a.Reset()

	var s argsScanner
	s.b = b

	var kv *argsKV
	a.args, kv = allocArg(a.args)
	for s.next(kv) {
		if len(kv.key) > 0 || len(kv.value) > 0 {
			a.args, kv = allocArg(a.args)
		}
	}
	a.args = releaseArg(a.args)
}

// 字串化
func (a *Args) String() string {
	return string(a.QueryString())
}

// buf从args重组化
func (a *Args) QueryString() []byte {
	a.buf = a.AppendBytes(a.buf[:0])
	return a.buf
}

// 将args键值，遍历传给dst
func (a *Args) AppendBytes(dst []byte) []byte {
	for i, n := 0, len(a.args); i < n; i++ {
		kv := &a.args[i]
		dst = AppendQuotedArg(dst, kv.key)
		if len(kv.value) > 0 {
			dst = append(dst, '=')
			dst = AppendQuotedArg(dst, kv.value)
		}
		if i+1 < n {
			dst = append(dst, '&')
		}
	}
	return dst
}

func (a *Args) WriteTo(w io.Writer) (int64, error) {
	n, err := w.Write(a.QueryString())
	return int64(n), err
}

// --- del key
func (a *Args) Del(key string) {
	a.args = delAllArgs(a.args, key)
}
func (a *Args) DelBytes(key []byte) {
	a.args = delAllArgs(a.args, b2s(key))
}

// --- add key, value:复制值方式
func (a *Args) Add(key, value string) {
	a.args = appendArg(a.args, key, value)
}

func (a *Args) AddBytesK(key []byte, value string) {
	a.args = appendArg(a.args, b2s(key), value)
}

func (a *Args) AddBytesV(key string, value []byte) {
	a.args = appendArg(a.args, key, b2s(value))
}

func (a *Args) AaddBytesKV(key, value []byte) {
	a.args = appendArg(a.args, b2s(key), b2s(value))
}

// --- set key, value:复制值方式
func (a *Args) Set(key, value string) {
	a.args = setArg(a.args, key, value)
}

func (a *Args) SetBytesK(key []byte, value string) {
	a.args = setArg(a.args, b2s(key), value)
}

func (a *Args) SetBytesV(key string, value []byte) {
	a.args = setArg(a.args, key, b2s(value))
}

func (a *Args) SetBytesKV(key, value []byte) {
	a.args = setArg(a.args, b2s(key), b2s(value))
}

// --- peek key
func (a *Args) Peek(key string) []byte {
	return peekArgStr(a.args, key)
}

func (a *Args) PeekBytes(key []byte) []byte {
	return peekArgBytes(a.args, key)
}

func (a *Args) PeekMulti(key string) [][]byte {
	var values [][]byte
	a.VisitAll(func(k, v []byte) {
		if string(k) == key {
			values = append(values, v)
		}
	})
	return values
}

func (a *Args) PeekMultiBytes(key []byte) [][]byte {
	return a.PeekMulti(b2s(key))
}

// --- has key
func (a *Args) Has(key string) bool {
	return hasArg(a.args, key)
}

func (a *Args) HasBytes(key []byte) bool {
	return hasArg(a.args, b2s(key))
}

// --- proc value Uint
func (a *Args) GetUint(key string) (int, error) {
	value := a.Peek(key)
	if len(value) == 0 {
		return -1, ErrNoArgValue
	}
	return ParseUint(value)
}

func (a *Args) SetUint(key string, value int) {
	bb := AcquireByteBuffer()
	bb.B = AppendUint(bb.B[:0], value)
	a.SetBytesV(key, bb.B)
	ReleaseByteBuffer(bb)
}

func (a *Args) SetUintBytes(key []byte, value int) {
	a.SetUint(b2s(key), value)
}

func (a *Args) GetUintOrZero(key string) int {
	n, err := a.GetUint(key)
	if err != nil {
		n = 0
	}
	return n
}

func (a *Args) GetUfloat(key string) (float64, error) {
	value := a.Peek(key)
	if len(value) == 0 {
		return -1, ErrNoArgValue
	}
	return ParseUfloat(value)
}

func (a *Args) GetUfloatOrZero(key string) float64 {
	f, err := a.GetUfloat(key)
	if err != nil {
		f = 0
	}
	return f
}

func (a *Args) GetBool(key string) bool {
	switch string(a.Peek(key)) {
	case "1", "y", "yes": // todo true Y YeS TrUe
		return true
	default:
		return false
	}
}

//===============================
// args解析器
type argsScanner struct {
	b []byte
}

// todo html传数组的支持-[]解析
// 1.空值处理
// 2.用=&判定键值界限
func (s *argsScanner) next(kv *argsKV) bool {
	if len(s.b) == 0 {
		return false
	}

	isKey := true
	k := 0
	for i, c := range s.b {
		switch c {
		case '=':
			if isKey {
				isKey = false
				kv.key = decodeArgAppend(kv.key[:0], s.b[:i])
				k = i + 1
			}
		case '&':
			if isKey { //未找到=xx字串,将其设为key=null
				kv.key = decodeArgAppend(kv.key[:0], s.b[:i])
				kv.value = kv.value[:0]
			} else { //找到该value结尾
				kv.value = decodeArgAppend(kv.value[:0], s.b[:i])
			}
			s.b = s.b[i+1:]
			return true //找到一对"键值对"
		}
	}

	if isKey { //剩余字串中，未找到=和&字符，当成key值
		kv.key = decodeArgAppend(kv.key[:0], s.b)
		kv.value = kv.value[:0]
	} else { // 剩余字串中，未找到&字符，当成value值
		kv.value = decodeArgAppend(kv.value[:0], s.b[k:])
	}
	s.b = s.b[len(s.b):]
	return true
}

// =============================

func visitArgs(args []argsKV, f func(k, v []byte)) {
	for i, n := 0, len(args); i < n; i++ {
		kv := &args[i]
		f(kv.key, kv.value)
	}
}

// 1.比较并处理大小，使dst与src相等
// 2.遍历，复制值(append)
func copyArgs(dst, src []argsKV) []argsKV {
	n := len(src)
	if cap(dst) < n {
		tmp := make([]argsKV, n)
		//copy(tmp, dst) // 没必要, dst数据被src覆盖
		dst = tmp
	} else {
		dst = dst[:n]
	}

	for i := 0; i < n; i++ {
		dstKV := &dst[i]
		srcKV := &src[i]
		dstKV.key = append(dstKV.key[:0], srcKV.key...)
		dstKV.value = append(dstKV.value[:0], srcKV.value...)
	}

	return dst
}

// del操作
func delAllArgsBytes(args []argsKV, key []byte) []argsKV {
	return delAllArgs(args, b2s(key))
}

// del操作 所有重复key的
// 1.找到
// 2.前移
// 3.将删除的值，移到末尾(不可见),而非直接删除 todo?
func delAllArgs(args []argsKV, key string) []argsKV {
	for i, n := 0, len(args); i < n; i++ {
		kv := &args[i]
		if key == string(kv.key) {
			tmp := *kv
			copy(args[i:], args[i+1:])
			n--
			args[n] = tmp
			args = args[:n]
		}
	}
	return args
}

func setArgBytes(h []argsKV, key, value []byte) []argsKV {
	return setArg(h, b2s(key), b2s(value))
}

// 先尝试找到1个，并赋值
// 找不到，就添加1个
func setArg(h []argsKV, key, value string) []argsKV {
	for i, n := 0, len(h); i < n; i++ {
		kv := &h[i]
		if key == string(kv.key) {
			kv.value = append(kv.value[:0], value...)
			return h
		}
	}
	return appendArg(h, key, value)
}

func appendArgBytes(h []argsKV, key, value []byte) []argsKV {
	return appendArg(h, b2s(key), b2s(value))
}

// 先分配1个栏位，再复制key,value值
func appendArg(args []argsKV, key, value string) []argsKV {
	var kv *argsKV
	args, kv = allocArg(args)
	kv.key = append(kv.key[:0], key...)
	kv.value = append(kv.value[:0], value...)
	return args
}

// args添加1栏位
func allocArg(h []argsKV) ([]argsKV, *argsKV) {
	n := len(h)
	if cap(h) > n {
		h = h[:n+1]
	} else {
		h = append(h, argsKV{})
	}
	return h, &h[n]
}

// args移除1栏位
func releaseArg(h []argsKV) []argsKV {
	return h[:len(h)-1] //引用操作
}

// 检测是否存在
func hasArg(h []argsKV, key string) bool {
	for i, n := 0, len(h); i < n; i++ {
		kv := &h[i]
		if key == string(kv.key) {
			return true
		}
	}
	return false
}

// *直接返回引用
func peekArgBytes(h []argsKV, k []byte) []byte {
	for i, n := 0, len(h); i < n; i++ {
		kv := &h[i]
		if bytes.Equal(kv.key, k) {
			return kv.value
		}
	}
	return nil
}

// *直接返回引用
// todo 使用peekArgBytes or this
func peekArgStr(h []argsKV, k string) []byte {
	k_tmp := s2b(k)
	for i, n := 0, len(h); i < n; i++ {
		kv := &h[i]
		if string(kv.key) == k {
			return kv.value
		}
	}
	return nil
}

// 将src解码，并赋到dst后
func decodeArgAppend(dst, src []byte) []byte {
	if bytes.IndexByte(src, '%') < 0 && bytes.IndexByte(src, '+') < 0 {
		// src不包含转码字符
		return append(dst, src...)
	}

	for i := 0; i < len(src); i++ {
		c := src[i]
		if c == '%' {
			if i+2 >= len(src) { // 结尾
				return append(dst, src[i:]...)
			}
			x2 := hex2intTable[src[i+2]]
			x1 := hex2intTable[src[i+1]]
			if x1 == 16 || x2 == 16 {
				dst = append(dst, '%')
			} else {
				dst = append(dst, x1<<4|x2)
				i += 2
			}
		} else if c == '+' {
			dst = append(dst, ' ')
		} else {
			dst = append(dst, c)
		}
	}
	return dst
}

// 与decodeArgAppend保持一致，除了不解码'+'
// 仅因为性能原因，这个函数从decodeArgAppend复制过来??
func decodeArgAppendNoPlus(dst, src []byte) []byte {
	if bytes.IndexByte(src, '%') < 0 {
		// src不包含转码字符
		return append(dst, src...)
	}

	for i := 0; i < len(src); i++ {
		c := src[i]
		if c == '%' {
			if i+2 >= len(src) { // 结尾
				return append(dst, src[i:]...)
			}
			x2 := hex2intTable[src[i+2]]
			x1 := hex2intTable[src[i+1]]
			if x1 == 16 || x2 == 16 {
				dst = append(dst, '%')
			} else {
				dst = append(dst, x1<<4|x2)
				i += 2
			}
		} else {
			dst = append(dst, c)
		}
	}
	return dst
}
