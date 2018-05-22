package selfFastHttp

// interfaces for header.go -> Request

// 'Referer: https://www.baidu.com/link?url=MXWDtIsobneu62Hfeyllwficn4yCAv'

// 是否设置:'Connection: close'
func (h *RequestHeader) ConnectionClose() bool {
	h.parseRawHeader()
	return h.connectionClose
}

// 简化版-按需
func (h *RequestHeader) ConnectionCloseFast() bool {
	return h.connectionClose
}

func (h *RequestHeader) SetConnectionClose() {
	// 考虎性能原因，未设用h.parseRawHeaders()
	h.connectionClose = true
}

// 如果已设'Connection: close',清理之
func (h *RequestHeader) ReSetConnectionClose() {
	h.parseRawHeader()
	if h.connectionClose {
		h.connectionClose = false
		h.h = delAllArgsBytes(h.h, strConnection)
	}
}
