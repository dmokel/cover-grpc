package main

import (
	"log"
	"net/http"

	drpc "github.com/dmokel/dprc"
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
	if err := drpc.Register(&foo); err != nil {
		log.Fatal("register failed, error: ", err)
	}
	drpc.HandleHTTP()

	http.HandleFunc("/debug", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html>hello</html>"))
		return
	})

	http.ListenAndServe("127.0.0.1:9999", nil)
}
