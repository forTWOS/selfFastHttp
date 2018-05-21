package selfFastHttp

import (
	"errors"
	"math"
	"reflect"
	"unsafe"
)

// 将int转成[]byte
// 循环判断传入值:
//    1.>=10 将个位数转成ascii码，存入临时缓存buf中
//    2.最后一位，也保存进缓存buf中
// 将缓存附加到dst后
func AppendUint(dst []byte, n int) []byte {
	if n < 0 {
		panic("BUG: int must be positive")
	}

	var b [20]byte
	buf := b[:]
	i := len(buf)
	var q int
	for n >= 10 {
		i--
		q = n / 10
		buf[i] = '0' + byte(n-q*10)
		n = q
	}
	i--
	buf[i] = '0' + byte(n)
	dst = append(dst, buf[i:]...)
	return dst
}

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
// 1.长度判断
// 2.将字符ascii值转对应0-9数值(c - '0')
// 3.字符非法判定 k > 9 (byte=uint8，负号变正数)
// 4.数值最长字串判定
func parseUintBuf(b []byte) (int, int, error) {
	n := len(b)
	if n == 0 {
		return -1, 0, errEmptyInt
	}
	v := 0
	for i := 0; i < n; i++ {
		c := b[i]
		k := c - '0'
		if 0 < k || k > 9 { //非法字符
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
	errTooLongFloat64       = errors.New("too long char in float number")
)

// 将[]byte转为float64，粗略版
// 1.空值判断
// 2.遍历：
//   2.1.0-9, 按10进制转成float64
//   2.2.非0-9：
//      2.2.1. 若为'.'，若重复出现，返回错误;否则加标志-后续小数值控制
//      2.2.2. 若为'e'，'E'，判断若无后续字符,报错;否则，后续字串转10进制，用于前面值指数运算
// 粗略定为最长19，超过即为不支持的数据
func ParseUfloat(buf []byte) (float64, error) {
	if len(buf) == 0 {
		return -1, errEmptyFloat
	}
	b := buf
	var v uint64
	var offset = 1.0
	var pointFound bool

	for i, c := range b {
		if i > 19 {
			return -1, errTooLongFloat64
		}
		if c < '0' || c > '9' {
			if c == '.' {
				if pointFound {
					return -1, errDuplicateFloatPoint
				}
				pointFound = true
				continue
			}
			if c == 'e' || c == 'E' {
				if i+1 > len(b) {
					return -1, errUnexpectedFloatEnd
				}
				b = b[i+1:]
				minus := -1 //正负号
				switch b[0] {
				case '+':
					b = b[1:]
					minus = 1
				case '-':
					b = b[1:]
				default:
					minus = 1
				}
				vv, err := ParseUint(b)
				if err != nil {
					return -1, errInvalidFloatExponent
				}
				return float64(v) / offset * math.Pow10(minus*int(vv)), nil

			}
			return -1, errUnexpectedFloatChar
		}
		v = v*10 + uint64(c-'0')
		if pointFound {
			offset *= 10 // 1.0/10000会转1e-05
		}
	}
	// offset转成科学计数后，与值做运算，会不精确 123 * 1e-05 = 0.0012300000000000002
	return float64(v) / offset, nil //精确性-0.00123 == 0.0012300000000000002
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

// -------------------
const toLower = 'a' - 'A'

// 256字符中 A-Z转成a-z
var toLowerTable = func() [256]byte {
	var a [256]byte
	for i := 0; i < 256; i++ {
		c := byte(i)
		if c >= 'A' && c <= 'Z' {
			c += toLower
		}
		a[i] = c
	}
	return a
}()

// 256字符中 a-z转成A-Z
var toUpperTable = func() [256]byte {
	var a [256]byte
	for i := 0; i < 256; i++ {
		c := byte(i)
		if c >= 'a' && c <= 'z' {
			c -= toLower
		}
		a[i] = c
	}
	return a
}()

func lowercaseBytes(b []byte) {
	for i, n := 0, len(b); i < n; i++ {
		p := &b[i]
		*p = toLowerTable[*p]
	}
}

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

// 将src经url-encoded转换,传给dst
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

// 按路径格式，将传入src转码到dst
func appendQuotedPath(dst, src []byte) []byte {
	for _, c := range src {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '/' || c == '.' || c == ',' || c == '=' || c == ':' || c == '&' || c == '~' || c == '-' || c == '_' {
			dst = append(dst, c)
		} else {
			dst = append(dst, '%', hexCharUpper(c>>4), hexCharUpper(c&15))
		}
	}
	return dst
}
