package selfFastHttp

import (
	"bytes"
	"fmt"
)

func TestHello() []byte {
	print("Hello")
	return []byte("World!")
}
func print(fmtstr string, argv ...interface{}) {
	var buf bytes.Buffer
	now := CoarseTimeNow()
	buf.WriteString(now.String())
	buf.WriteString(" : ")
	str := fmt.Sprintf(fmtstr, argv...)
	buf.WriteString(str)
	fmt.Println(buf.String())
}
