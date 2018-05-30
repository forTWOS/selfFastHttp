package selfFastHttp

import (
	"github.com/valyala/bytebufferpool"
)

type ByteBuffer bytebufferpool.ByteBuffer

func (b *ByteBuffer) Write(p []byte) (int, error) {
	return 0, nil
}

func AcquireByteBuffer() *ByteBuffer {
	return nil
}

func ReleaseByteBuffer(b *ByteBuffer) {

}
