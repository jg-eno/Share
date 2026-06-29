package server

import (
	"fmt"
	"net/http"
)

func Start(root string, port int) error {
	addr := fmt.Sprintf(":%d", port)

	fs := http.FileServer(http.Dir(root))

	return http.ListenAndServe(addr, fs)
}