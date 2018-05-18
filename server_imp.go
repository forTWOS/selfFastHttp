package selfFastHttp

import (
	"io"
	"net"
	"time"
)

func ListenAndServe(addr string, handler RequestHandler) error {
	s := &Server{
		Handler: handler,
	}
	return s.ListenAndServe(addr)
}

func (s *Server) ListenAndServe(addr string) error {
	ln, err := net.Listen("tcp4", addr)
	if err != nil {
		return err
	}
	return s.Serve(ln)
}

func (s *Server) Serve(ln net.Listener) error {
	var c net.Conn
	var err error
	var lastPerIPErrorTime time.Time

	maxWorkersCount := s.getConcurrency()
	s.concurrencyCh = make([]chan struct{}, maxWorkersCount)
	wp := &workerPool{
		//ServeFunc:       s.serveConn,//todo
		MaxWorkersCount: maxWorkersCount,
		LogAllErrors:    s.LogAllErrors,
		Logger:          s.logger(),
	}
	wp.Start()

	for {
		if c, err = acceptConn(s, ln, &lastPerIPErrorTime); err != nil {
			wp.Stop()
			if err == io.EOF {
				return nil
			}
		}
		if !wp.Serve(c) {
			s.writeFastError(c, StatusServiceUnavailable, "The connection cannot be served because Server.Concurrency limit exceeded")
			c.Close()
		}
	}
	return nil
}
