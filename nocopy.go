package selfFastHttp

type noCopy struct{}

func (*noCopy) Lock() {}
