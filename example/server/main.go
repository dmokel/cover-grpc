package main

import (
	"log"
	"net"

	covergrpc "github.com/dmokel/cover-grpc"
)

// Foo ...
type Foo struct {
}

// Args ...
type Args struct{ Num1, Num2 int }

// Sum ...
func (f *Foo) Sum(args Args, reply *int) error {
	*reply = args.Num1 + args.Num2
	return nil
}

func main() {
	var foo Foo
	if err := covergrpc.Register(&foo); err != nil {
		log.Fatal("register failed, error: ", err)
	}

	l, err := net.Listen("tcp", "127.0.0.1:9999")
	if err != nil {
		log.Fatal("network error:", err)
	}
	log.Println(covergrpc.Serve(l))
}
