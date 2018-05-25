package selfFastHttp

// interfaces for http.go

// --- URI
func (req *Request) URI() *URI {
	req.parseURI()
	return &req.uri
}
func (req *Request) parseURI() {
	if req.parsedURI {
		return
	}
	req.parsedURI = true

	// 从header中，获取元数据，用于解析
	req.uri.ParseQuick(req.Header.RequestURI(), &req.Header, req.isTLS)
}

// --- Host
func (req *Request) SetHost(host string) {
	req.URI().SetHost(host)
}

func (req *Request) SetHostBytes(host []byte) {
	req.URI().SetHostBytes(host)
}

// --- RequestURI
// 仅uri,path+query+fragment
func (req *Request) SetRequestURI(requestURI string) {
	req.Header.SetRequestURI(requestURI)
	req.parsedURI = false
}
func (req *Request) SetRequestURIBytes(requestURI []byte) {
	req.Header.SetRequestURIBytes(requestURI)
	req.parsedURI = false
}
func (req *Request) RequestURI() []byte {
	if req.parsedURI {
		requestURI := req.uri.RequestURI()
		req.SetRequestURIBytes(requestURI)
	}
	return req.Header.RequestURI()
}

// --- 响应头 'Connection: close'
// 是否已设
func (req *Request) ConnectionClose() bool {
	return req.Header.ConnectionClose()
}

// 设置之
func (req *Request) SetConnectionClose() {
	req.Header.SetConnectionClose()
}
