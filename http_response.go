package selfFastHttp

// interfaces for http.go

// --- 状态码
func (resp *Response) StatusCode() int {
	return resp.Header.StatusCode()
}
func (resp *Response) SetStatusCode(statusCode int) {
	resp.Header.SetStatusCode(statusCode)
}

// --- 'Connection: close' 响应头
// 是否已设置
func (resp *Response) ConnectionClose() bool {
	return resp.Header.ConnectionClose()
}

// 设置响应头:'Connection: close'
func (resp *Response) SetConnectionClose() {
	resp.Header.SetConnectionClose()
}
