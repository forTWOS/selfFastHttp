package selfFastHttp

import (
	"bytes"
	/*"compress/flate"
	"compress/gzip"
	"compress/zlib"*/
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/klauspost/compress/flate"
	"github.com/klauspost/compress/gzip"
	"github.com/klauspost/compress/zlib"
	"github.com/valyala/bytebufferpool"
	"github.com/valyala/fasthttp/stackless"
)

const (
	CompressNoCompression      = flate.NoCompression
	CompressBestSpeed          = flate.BestSpeed
	CompressBestCompression    = flate.BestCompression
	CompressDefaultCompression = 6  // flate.DefaultCompression
	CompressHuffmanOnly        = -2 // flate.HuffmanOnly
)

// --- gzip
func acquireGzipReader(r io.Reader) (*gzip.Reader, error) {
	v := gzipReaderPool.Get()
	if v == nil {
		return gzip.NewReader(r)
	}
	zr := v.(*gzip.Reader)
	if err := zr.Reset(r); err != nil {
		return nil, err
	}
	return zr, nil
}
func releaseGzipReader(zr *gzip.Reader) {
	zr.Close()
	gzipReaderPool.Put(zr)
}

var gzipReaderPool sync.Pool

// --- flate
func acquireFlateReader(r io.Reader) (io.ReadCloser, error) {
	v := flateReaderPool.Get()
	if v == nil {
		zr, err := zlib.NewReader(r)
		if err != nil {
			return nil, err
		}
		return zr, nil
	}
	zr := v.(io.ReadCloser)
	if err := resetFlateReader(zr, r); err != nil {
		return nil, err
	}
	return zr, nil
}
func releaseFlateReader(zr io.ReadCloser) {
	zr.Close()
	flateReaderPool.Put(zr)
}

func resetFlateReader(zr io.ReadCloser, r io.Reader) error {
	zrr, ok := zr.(zlib.Resetter)
	if !ok {
		panic("BUG: zlib.Reader doesn't implement zlib.Resetter???")
	}
	return zrr.Reset(r, nil)
}

var flateReaderPool sync.Pool

// --- StackLessGzipWriter
// 按压缩等级，获取一个压缩器
func acquireStacklessGzipWriter(w io.Writer, level int) stackless.Writer {
	nLevel := normalizeCompressLevel(level)
	p := stacklessGzipWriterPoolMap[nLevel]
	v := p.Get()
	if v == nil {
		return stackless.NewWriter(w, func(w io.Writer) stackless.Writer {
			return acquireRealGzipWriter(w, level)
		})
	}
	sw := v.(stackless.Writer)
	sw.Reset(w)
	return sw
}
func releaseStacklessGzipWriter(sw stackless.Writer, level int) {
	sw.Close()
	nLevel := normalizeCompressLevel(level)
	p := stacklessGzipWriterPoolMap[nLevel]
	p.Put(sw)
}

// --- realGzipWriter
func acquireRealGzipWriter(w io.Writer, level int) *gzip.Writer {
	nLevel := normalizeCompressLevel(level)
	p := realGzipWriterPoolMap[nLevel]
	v := p.Get()
	if v == nil {
		zw, err := gzip.NewWriterLevel(w, level)
		if err != nil {
			panic(fmt.Sprintf("BUG: unexpected error from gzip.NewWriterLevel(%d): %s", level, err))
		}
		return zw
	}
	zw := v.(*gzip.Writer)
	zw.Reset(w)
	return zw
}
func releaseRealGzipWriter(zw *gzip.Writer, level int) {
	zw.Close()
	nLevel := normalizeCompressLevel(level)
	p := realGzipWriterPoolMap[nLevel]
	p.Put(zw)
}

var (
	stacklessGzipWriterPoolMap = newCompressWriterPoolMap()
	realGzipWriterPoolMap      = newCompressWriterPoolMap()
)

// 将p gzip到w中
// level:
// * CompressNoCompression
// * CompressBestSpeed
// * CompressBestCompression
// * CompressDefaultCompression
// * CompressHuffmanOnly
func AppendGzipBytesLevel(dst, src []byte, level int) []byte {
	w := &byteSliceWriter{dst}
	WriteGzipLevel(w, src, level)
	return w.b
}

// 将p gzip到w中
// level:
// * CompressNoCompression
// * CompressBestSpeed
// * CompressBestCompression
// * CompressDefaultCompression
// * CompressHuffmanOnly
func WriteGzipLevel(w io.Writer, p []byte, level int) (int, error) {
	switch w.(type) {
	case *byteSliceWriter,
		*bytes.Buffer,
		//*ByteBuffer,
		*bytebufferpool.ByteBuffer:
		ctx := &compressCtx{
			w:     w,
			p:     p,
			level: level,
		}
		stacklessWriteGzip(ctx)
		return len(p), nil
	default:
		zw := acquireStacklessGzipWriter(w, level)
		n, err := zw.Write(p)
		releaseStacklessGzipWriter(zw, level)
		return n, err
	}
}

var stacklessWriteGzip = stackless.NewFunc(nonblockingWriteGzip)

func nonblockingWriteGzip(ctxv interface{}) {
	ctx := ctxv.(*compressCtx)
	zw := acquireRealGzipWriter(ctx.w, ctx.level)

	_, err := zw.Write(ctx.p)
	if err != nil {
		panic(fmt.Sprintf("BUG: gzip.Writer.Write for len(p)=%d returned unexpected error: %s", len(ctx.p), err))
	}

	releaseRealGzipWriter(zw, ctx.level)
}

func WriteGzip(w io.Writer, p []byte) (int, error) {
	return WriteGzipLevel(w, p, CompressDefaultCompression)
}

func AppendGzipBytes(dst, src []byte) []byte {
	return AppendGzipBytesLevel(dst, src, CompressDefaultCompression)
}

// 解p解压到w,并返回解压后写的w的数据大小
func WriteGunzip(w io.Writer, p []byte) (int, error) {
	r := &byteSliceReader{p}
	zr, err := acquireGzipReader(r) //获取一个Gunzip读取器,Read接口中实现解压
	if err != nil {
		return 0, err
	}
	n, err := copyZeroAlloc(w, zr) //zr.Read时，解压
	releaseGzipReader(zr)
	nn := int(n)
	if int64(nn) != n {
		return 0, fmt.Errorf("too much data gunzipped: %d", n)
	}
	return nn, err
}

func AppendGunzipBytes(dst, src []byte) ([]byte, error) {
	w := &byteSliceWriter{dst}
	_, err := WriteGunzip(w, src)
	return w.b, err
}

// 将p deflate到w中
// level:
// * CompressNoCompression
// * CompressBestSpeed
// * CompressBestCompression
// * CompressDefaultCompression
// * CompressHuffmanOnly
func AppendDeflateBytesLevel(dst, src []byte, level int) []byte {
	w := &byteSliceWriter{dst}
	WriteDeflateLevel(w, src, level)
	return w.b
}

// 将p deflate到w中
// level:
// * CompressNoCompression
// * CompressBestSpeed
// * CompressBestCompression
// * CompressDefaultCompression
// * CompressHuffmanOnly
func WriteDeflateLevel(w io.Writer, p []byte, level int) (int, error) {
	switch w.(type) {
	case *byteSliceWriter,
		*bytes.Buffer,
		//*ByteBuffer,
		*bytebufferpool.ByteBuffer:
		ctx := &compressCtx{
			w:     w,
			p:     p,
			level: level,
		}
		stacklessWriteDeflate(ctx)
		return len(p), nil
	default:
		zw := acquireStacklessDeflateWriter(w, level)
		n, err := zw.Write(p)
		releaseStacklessDeflateWriter(zw, level)
		return n, err
	}
}

var stacklessWriteDeflate = stackless.NewFunc(nonblockingWriteDeflate)

// 无阻塞压缩处理数据
func nonblockingWriteDeflate(ctxv interface{}) {
	ctx := ctxv.(*compressCtx)
	zw := acquireRealDeflateWriter(ctx.w, ctx.level)

	_, err := zw.Write(ctx.p)
	if err != nil {
		panic(fmt.Sprintf("BUG: zlib.Writer.Write for len(p)=%d returned unexpected error: %s", len(ctx.p), err))
	}

	releaseRealDeflateWriter(zw, ctx.level)
}

type compressCtx struct {
	w     io.Writer // 要输出到的出口
	p     []byte    // 要处理的数据
	level int       // 处理等级
}

func WriteDeflate(w io.Writer, p []byte) (int, error) {
	return WriteDeflateLevel(w, p, CompressDefaultCompression)
}

func AppendDeflateBytes(dst, src []byte) []byte {
	return AppendDeflateBytesLevel(dst, src, CompressDefaultCompression)
}

// 将p inflate到w中
func WriteInflate(w io.Writer, p []byte) (int, error) {
	r := &byteSliceReader{p}
	zr, err := acquireFlateReader(r)
	if err != nil {
		return 0, err
	}
	n, err := copyZeroAlloc(w, zr)
	releaseFlateReader(zr)
	nn := int(n)
	if int64(nn) != n {
		return 0, fmt.Errorf("too much data inflated: %d", n)
	}
	return nn, err
}

// 将src inflate到 dst中
func AppendInflateBytes(dst, src []byte) ([]byte, error) {
	w := &byteSliceWriter{dst}
	_, err := WriteInflate(w, src)
	return w.b, err
}

// --- byteSliceWriter
type byteSliceWriter struct {
	b []byte
}

func (w *byteSliceWriter) Write(p []byte) (int, error) {
	w.b = append(w.b, p...)
	return len(p), nil
}

// --- byteSliceReader
type byteSliceReader struct {
	b []byte
}

func (r *byteSliceReader) Read(p []byte) (int, error) {
	if len(r.b) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.b)
	r.b = r.b[n:]
	return n, nil
}

// --- stacklessDeflate
func acquireStacklessDeflateWriter(w io.Writer, level int) stackless.Writer {
	nLevel := normalizeCompressLevel(level)
	p := stacklessDeflateWriterPoolMap[nLevel]
	v := p.Get()
	if v == nil {
		return stackless.NewWriter(w, func(w io.Writer) stackless.Writer {
			return acquireRealDeflateWriter(w, level)
		})
	}
	sw := v.(stackless.Writer)
	sw.Reset(w)
	return sw
}
func releaseStacklessDeflateWriter(sw stackless.Writer, level int) {
	sw.Close()
	nLevel := normalizeCompressLevel(level)
	p := stacklessDeflateWriterPoolMap[nLevel]
	p.Put(sw)
}

// --- realDeflate
func acquireRealDeflateWriter(w io.Writer, level int) *zlib.Writer {
	nLevel := normalizeCompressLevel(level)
	p := realDeflateWriterPoolMap[nLevel]
	v := p.Get()
	if v == nil {
		zw, err := zlib.NewWriterLevel(w, level)
		if err != nil {
			panic(fmt.Sprintf("BUG: unexpected error from zlib.NewWriterLevel(%d): %s", level, err))
		}
		return zw
	}
	zw := v.(*zlib.Writer)
	zw.Reset(w)
	return zw
}
func releaseRealDeflateWriter(zw *zlib.Writer, level int) {
	zw.Close()
	nLevel := normalizeCompressLevel(level)
	p := realDeflateWriterPoolMap[nLevel]
	p.Put(zw)
}

var (
	stacklessDeflateWriterPoolMap = newCompressWriterPoolMap()
	realDeflateWriterPoolMap      = newCompressWriterPoolMap()
)

//=================

// 构造一个[0..11]等级的池数组
func newCompressWriterPoolMap() []*sync.Pool {
	var m []*sync.Pool
	for i := 0; i < 12; i++ {
		m = append(m, &sync.Pool{})
	}
	return m
}

func isFileCompressible(f *os.File, minCompressRatio float64) bool {
	// 尝试压缩前4kb数据，看其是否能达到 目标压缩比例
	b := AcquireByteBuffer()
	zw := acquireStacklessGzipWriter(b, CompressDefaultCompression)
	lr := &io.LimitedReader{
		R: f,    // 数据源
		N: 4096, // 最多读取的数据
	}
	_, err := copyZeroAlloc(zw, lr) //zw.Write时，解发压缩
	releaseStacklessGzipWriter(zw, CompressDefaultCompression)
	f.Seek(0, 0) //重置f
	if err != nil {
		return false
	}

	n := 4096 - lr.N
	zn := len(b.B)
	ReleaseByteBuffer(b)
	return float64(zn) < float64(n)*minCompressRatio
}

// 将gzip中的压缩等缓[-2..9]转化为 索引[0..11],用于池数组
func normalizeCompressLevel(level int) int {
	// -2 HuffmanOnly
	// 9 CompressBestCompression
	if level < -2 || level > 9 {
		level = CompressDefaultCompression
	}
	return level + 2
}
