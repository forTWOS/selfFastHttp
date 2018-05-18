package selfFastHttp

import (
	"reflect"
	"unsafe"
)

func ParseUint(buf []byte) (int, error) {
	v, n, err := parseUintBuf(buf)
	if n != len(buf) {
		return -1, ErrNoArgValue
	}
	return v, err
}

var (
	errEmptyInt               = errors.New("empty integer")
	errUnexpectedFirstChar    = errors.New("unexpected first char found. Expecting 0-9")
	errUnexpectedTrailingChar = errors.New("unexpected trailing char found. Expecting 0-9")
	errTooLongInt             = errors.New("too long int")
)

// 解析[]byte成int
func parseUintBuf(b []byte) (int, int, error) {
	n := len(b)
	if n == 0 {
		return -1, 0, errEmptyInt
	}
	v := 0
	for i := 0; i < n; i++ {
		c := b[i]
		k := c - '0'
		if k > 9 { //非法字符
			if i == 0 {
				return -1, i, errUnexpectedFirstChar
			}
			return v, i, nil
		}
		if i >= maxIntChars {
			return -1, i, errTooLongInt
		}
		v = 10*v + int(k)
	}
	return v, n, nil
}

var (
	errEmptyFloat           = errors.New("empty float number")
	errDuplicateFloatPoint  = errors.New("duplicate point found in float number")
	errUnexpectedFloatEnd   = errors.New("unexpected end of float number")
	errInvalidFloatExponent = errors.New("invalid float number exponent")
	errUnexpectedFloatChar  = errors.New("unexpected char found in float number")
)

func ParseUfloat(buf []byte) float64 {
	if len(buf) == 0 {
		return -1, errEmptyFloat
	}
	b := buf
	var v uint64
	var offset = 1.0
	var pointFound bool
	for i, c:= range b {
		if c < '0' || c > '9' {
			if c == '.' {
				if 
			}
		}
	}
}

// 将字符的十六进制转为十进制,其它字符保留原值
var hex2intTable = func() []byte {
	b := make([]byte, 256)
	for i := 0; i < 256; i++ {
		c := byte(16)
		if i >= '0' && i <= '9' {
			c = byte(i) - '0'
		} else if i >= 'a' && i <= 'f' {
			c = byte(i) - 'a' + 10
		} else if i >= 'A' && i <= 'F' {
			c = byte(i) - 'A' + 10
		}
		b[i] = c
	}
	return b
}()

// 用指针的方式，将[]byte转为string,绕过内存复制
// 失败:在将来版本中 string 和 slice 其中一个改变
func b2s(b []byte) string {
	return *(*string)(unsafe.Pointer(&b))
}

func s2b(s string) []byte {
	sh := (*reflect.StringHeader)(unsafe.Pointer(&s))
	bh := reflect.SliceHeader{
		Data: sh.Data,
		Len:  sh.Len,
		Cap:  sh.Len,
	}
	return *(*[]byte)(unsafe.Pointer(&bh))
}

func hexCharUpper(c byte) byte {
	if c < 10 {
		return '0' + c
	}
	return c - 10 + 'A'
}

// 将经过url-encoded转换的src传给dst
func AppendQuotedArg(dst, src []byte) []byte {
	for _, c := range src {
		// US-ASCII 2 UTF-8
		// See http://www.w3.org/TR/html5/forms.html#form-submission-algorithm
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '*' || c == '-' || c == '.' || c == '_' {
			//数字、字母、*、-、.、_
			dst = append(dst, c)
		} else {
			dst = append(dst, '%', hexCharUpper(c>>4), hexCharUpper(c&15))
		}
	}
	return dst
}
