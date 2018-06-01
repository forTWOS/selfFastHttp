package selfFastHttp

// files bigger than this size are sent with sendfile
const maxSmallFileSize = 2 * 4096

func ServeFile(ctx *RequestCtx, path string) {

}

func ServeFileBytes(ctx *RequestCtx, path []byte) {

}
