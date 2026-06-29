package server

import (
	"fmt"
	"net/http"
)

func (s *Server) Start() error {
	addr := fmt.Sprintf(":%d", s.Port)

	fs := http.FileServer(http.Dir(s.Root))

	return http.ListenAndServe(addr, fs)
}