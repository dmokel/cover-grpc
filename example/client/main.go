package main

import (
	"context"
	"log"
	"sync"

	drpc "github.com/dmokel/dprc"
)

// Args ...
type Args struct{ Num1, Num2 int }

func main() {
	client, err := drpc.DialHTTP("tcp", "127.0.0.1:9999")
	if err != nil {
		log.Printf("dial failed, error: %v", err)
		return
	}

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			args := Args{Num1: i, Num2: i * i}
			var reply int
			if err := client.Call(context.Background(), "Foo.Sum", args, &reply); err != nil {
				log.Fatal("call Foo.Sum error:", err)
			}
			log.Printf("%d + %d = %d", args.Num1, args.Num2, reply)
		}(i)
	}
	wg.Wait()
}
