// +build !windows

package selfFastHttp

func addLeadingSlash(dst, src []byte) []byte {
	if len(src) == 0 || src[0] != '/' {
		dst = append(dst, '/')
	}

	return dst
}
