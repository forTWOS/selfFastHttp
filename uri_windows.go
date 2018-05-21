// +build windows

package selfFastHttp

func addLeadingSlash(dst, src []byte) []byte {
	// 空值 或非 'C:/'
	if len(src) == 0 || (len(src) > 2 && src[1] != ':') {
		dst = append(dst, '/')
	}
	return dst
}
