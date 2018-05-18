package selfFastHttp

type ResponseHeader struct {
}

type RequestHeader struct {
}

func (r *RequestHeader) Method() []byte {
	return []byte("")
}
