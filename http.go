package selfFastHttp

type Request struct {
	Header RequestHeader
	URI    URI
}

type Response struct {
}

func (*Response) Reset() {

}
