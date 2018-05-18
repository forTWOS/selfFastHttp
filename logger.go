package selfFastHttp

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
)

type Logger interface {
	Printf(format string, args ...interface{})
}

var defaultLogger = Logger(log.New(os.Stderr, "", log.LstdFlags))

//自仿写Logger
type selfLogger struct {
	out io.Writer
}

func (l *selfLogger) Printf(format string, args ...interface{}) {
	var buf bytes.Buffer
	now := CoarseTimeNow()
	buf.WriteString(now.String())
	buf.WriteString(" : ")
	str := fmt.Sprintf(format, args...)
	buf.WriteString(str)
	//	fmt.Println(buf.String())
	l.out.Write(buf.Bytes())
}

func NewLogger(out io.Writer) *selfLogger {
	return &selfLogger{out: out}
}
