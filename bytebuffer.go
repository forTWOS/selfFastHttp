package selfFastHttp

import (
	"github.com/valyala/bytebufferpool"
)

func AcquireByteBuffer() *bytebufferpool.ByteBuffer {
	return defaultByteBufferPool.Get()
}

// ByteBuffer.B在此之后，不可被引用，否则数据竞争
func ReleaseByteBuffer(b *bytebufferpool.ByteBuffer) {
	defaultByteBufferPool.Put(b)
}

var defaultByteBufferPool bytebufferpool.Pool

/*
// 用于降低内存开销
// 用AcquireByteBuffer申请空buffer
// ByteBuffer将被淘汰，使用github.com/valyala/bytebufferpool
type ByteBuffer bytebufferpool.ByteBuffer

func (b *ByteBuffer) Write(p []byte) (int, error) {
	return bb(b).Write(p)
}

func (b *ByteBuffer) WriteString(s string) (int, error) {
	return bb(b).WriteString(s)
}

func (b *ByteBuffer) Set(p []byte) {
	bb(b).Set(p)
}
func (b *ByteBuffer) SetString(s string) {
	bb(b).SetString(s)
}

func (b *ByteBuffer) Reset() {
	bb(b).Reset()
}

/*func AcquireByteBuffer() *ByteBuffer {
	return (*ByteBuffer)(defaultByteBufferPool.Get())
}

// ByteBuffer.B在此之后，不可被引用，否则数据竞争
func ReleaseByteBuffer(b *ByteBuffer) {
	defaultByteBufferPool.Put(bb(b))
}

// 强转ByteBuffer->bytebufferpool.ByteBuffer
func bb(b *ByteBuffer) *bytebufferpool.ByteBuffer {
	return (*bytebufferpool.ByteBuffer)(b)
}

var defaultByteBufferPool bytebufferpool.Pool*/
