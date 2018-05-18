package selfFastHttp

//RequestCtx中玩家数据
//键值对 {[]byte : interface{}}

type userDataKV struct {
	key   []byte
	value interface{}
}
type userData []userDataKV
