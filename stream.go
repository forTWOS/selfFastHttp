package selfFastHttp

import (
	"bufio"
	"io"
	"sync"

	"github.com/forTWOS/selfFastHttp/selffasthttputil"
)

// 必定将数据写入w
// 通常，是循环遍历将数据写入w
// 当有错误时，需立即返回
// 因为是buffer数据，须调用Flush将数据传给reader
type StreamWriter func(w *bufio.Writer)

// 流读取器-协程异步方式
// 返回一个reader,用于重放sw产生的数据
// 这个reader有可能传到Response.SetBodyStream
// 返回reader中，当所有请求的数据被读完，须调用Close；否则goroutine有可能泄漏
func NewStreamReader(sw StreamWriter) io.ReadCloser {
	pc := selffasthttputil.NewPipeConns() //子包 todo??
	pw := pc.Conn1()
	pr := pc.Conn2()

	var bw *bufio.Writer
	v := streamWriterBufPool.Get()
	if v == nil {
		bw = bufio.NewWriter(pw)
	} else {
		bw = v.(*bufio.Writer)
		bw.Reset(pw)
	}

	go func() {
		sw(bw)
		bw.Flush()
		bw.Close()

		streamWriterBufPool.Put(bw)
	}()

	return pr
}

var streamWriterBufPool sync.Pool
