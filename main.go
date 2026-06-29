package main

import (
	"log"

	"share/internal/network"
	"share/internal/server"
)

func main() {
	ip, err := network.LocalIP()
	if err != nil {
		log.Fatal(err)
	}

	const (
		root = "/home/glenenosh/Documents/Books"
		port = 15016
	)

	log.Println("Server has started...")
	log.Printf("Address: http://%s:%d", ip, port)

	if err := server.Start(root, port); err != nil {
		log.Fatal(err)
	}
}