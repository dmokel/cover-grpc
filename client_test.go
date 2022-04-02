package covergrpc

import (
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/dmokel/cover-grpc/codec"
)

func TestClient_DialTimeout(t *testing.T) {
	t.Parallel()
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}

	f := func(rw io.ReadWriteCloser) (cc codec.Codec) {
		time.Sleep(time.Second * 2)
		return codec.NewGobCodec(rw)
	}

	t.Run("timeout", func(t *testing.T) {
		client := NewClient(&Option{
			MargicNumber:      DefaultOption.MargicNumber,
			CodecType:         DefaultOption.CodecType,
			ConnectionTimeout: time.Second,
		})
		conn, _ := net.Dial("tcp", l.Addr().String())
		_, err := client.initCodecTimeout(f, conn)
		_assert(err != nil && strings.Contains(err.Error(), "timeout"), "expect a timeout error")
	})

	t.Run("0", func(t *testing.T) {
		client := NewClient(&Option{
			MargicNumber:      DefaultOption.MargicNumber,
			CodecType:         DefaultOption.CodecType,
			ConnectionTimeout: 0,
		})
		conn, _ := net.Dial("tcp", l.Addr().String())
		_, err := client.initCodecTimeout(f, conn)
		_assert(err == nil, "0 means no limit")
	})
}

type Bar int

func (b *Bar) Timeout(argv int, replyv *int) error {
	time.Sleep(time.Second * 2)
	*replyv = argv * 2
	return nil
}

func startServer(addrCh chan string) {
	var bar Bar
	_ = Register(&bar)

	l, _ := net.Listen("tcp", ":0")
	addrCh <- l.Addr().String()
	_ = Serve(l)
}

func TestClient_CallTimeout(t *testing.T) {
	t.Parallel()
	addrCh := make(chan string)
	go startServer(addrCh)
	addr := <-addrCh
	t.Run("client timeout", func(t *testing.T) {
		client, _ := Dial("tcp", addr)
		ctx, _ := context.WithTimeout(context.Background(), time.Second)
		var reply int
		err := client.Call(ctx, "Bar.Timeout", 1, &reply)
		_assert(err != nil && strings.Contains(err.Error(), ctx.Err().Error()), "expect timeout err")
	})

	t.Run("server handle timeout", func(t *testing.T) {
		client, _ := Dial("tcp", addr, &Option{
			HandleTimeout: time.Second,
		})
		var reply int
		err := client.Call(context.Background(), "Bar.Timeout", 1, &reply)
		_assert(err != nil && strings.Contains(err.Error(), "handle timeout"), "expect handle timeout err")
	})
}
