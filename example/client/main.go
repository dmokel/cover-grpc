package main

import (
	"fmt"
	"log"
	"sync"

	covergrpc "github.com/dmokel/cover-grpc"
)

func main() {
	client, err := covergrpc.Dial("tcp", "127.0.0.1:9999")
	if err != nil {
		return
	}

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			args := fmt.Sprintf("geerpc req %d", i)
			var reply string
			if err := client.Call("Foo.Sum", args, &reply); err != nil {
				log.Fatal("call Foo.Sum error:", err)
			}
			log.Println("reply:", reply)
		}(i)
	}
	wg.Wait()
}
