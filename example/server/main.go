package main

import (
	"log"
	"net"

	covergrpc "github.com/dmokel/cover-grpc"
)

func main() {
	l, err := net.Listen("tcp", "127.0.0.1:9999")
	if err != nil {
		log.Fatal("network error:", err)
	}
	log.Println(covergrpc.Serve(l))
}
