package selfFastHttp

import (
	"github.com/valyala/bytebufferpool"
)

type ByteBuffer bytebufferpool.ByteBuffer

func AcquireByteBuffer() *ByteBuffer {
	return nil
}

func ReleaseByteBuffer(b *ByteBuffer) {

}
