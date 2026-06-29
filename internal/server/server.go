package server

type Server struct {
	Root string
	Port int
}

func New(root string, port int) *Server {
	return &Server{
		Root: root,
		Port: port,
	}
}
