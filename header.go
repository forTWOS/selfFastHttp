package selfFastHttp

type ResponseHeader struct {
}

type RequestHeader struct {
}

func (r *RequestHeader) Method() []byte {
	return []byte("")
}
func (r *RequestHeader) CopyTo(dst *RequestHeader) {

}
func (r *RequestHeader) Reset() {

}
func (r *RequestHeader) HOST() []byte {
	return nil
}
