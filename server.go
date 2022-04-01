package covergrpc

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"reflect"
	"sync"

	"github.com/dmokel/cover-grpc/codec"
)

// MagicNumber ...
// define the unique magic number for cover-grpc
const magicNumber = 0x3bef5c

// Option ...
type Option struct {
	MargicNumber int        // MagicNumber marks this's a cover-grpc rpcPackage
	CodecType    codec.Type // client may choose different Codec to encode body
}

// DefaultOption ...
var DefaultOption = &defaultOption
var defaultOption = Option{
	MargicNumber: magicNumber,
	CodecType:    codec.GobType,
}

// Server ...
type Server struct{}

// NewServer ...
func NewServer() *Server {
	return &Server{}
}

// Serve ...
func (s *Server) Serve(l net.Listener) error {
	log.Println("rpc server: Serve success")
	for {
		rw, err := l.Accept()
		if err != nil {
			log.Printf("rpc server: accept failed, err: %v\n", err)
			return err
		}

		conn := s.newConn(rw)
		if conn == nil {
			log.Println("rpc server: new conn failed")
			rw.Close()
			continue
		}
		go conn.Serve()
	}
}

// defaultServer ...
var defaultServer = NewServer()

// Serve ...
func Serve(l net.Listener) error { return defaultServer.Serve(l) }

func (s *Server) newConn(rwc io.ReadWriteCloser) *conn {
	var opt Option
	if err := json.NewDecoder(rwc).Decode(&opt); err != nil {
		log.Printf("rpc server: decode option failed, err: %v\n", err)
		return nil
	}
	if opt.MargicNumber != magicNumber {
		log.Printf("rpc server: invalid magic number, err: %v\n", opt.MargicNumber)
		return nil
	}
	f := codec.NewCodecFuncMap[opt.CodecType]
	if f == nil {
		log.Printf("rpc server: invalid codec type, err: %v\n", opt.CodecType)
		return nil
	}
	cc := f(rwc)

	return &conn{
		srv: s,
		cc:  cc,
	}
}

// conn represent the rpc connection
type conn struct {
	srv    *Server
	client *Client
	cc     codec.Codec
}

// rpcPackage stores all information of a call
type rpcPackage struct {
	conn         *conn
	h            *codec.Header // header of rpcPackage
	argv, replyv reflect.Value // argv and replyv of rpcPackage
}

func (c *conn) close() error {
	return c.cc.Close()
}

// invalidPackage is a placeholder for response argv when error occurs
var invalidPackage = struct{}{}

func (c *conn) Serve() {
	defer func() { _ = c.close() }()

	sending := new(sync.Mutex)
	wg := new(sync.WaitGroup)
	for {
		pkg, err := c.readPackage()
		if err != nil {
			if pkg == nil {
				break // it's not possible to recover, so close the connection
			}
			pkg.h.Error = err.Error()
			c.sendPackage(pkg.h, invalidPackage, sending)
			continue
		}
		wg.Add(1)
		go c.handlePackage(pkg, wg, sending)
	}
	wg.Wait()
}

func (c *conn) readPackage() (*rpcPackage, error) {
	var h codec.Header
	if err := c.cc.ReadHeader(&h); err != nil {
		if err != io.EOF && err != io.ErrUnexpectedEOF {
			log.Printf("rpc server: read header failed, err: %v\n", err)
		}
		return nil, err
	}

	pkg := &rpcPackage{
		conn: c,
		h:    &h,
	}

	pkg.argv = reflect.New(reflect.TypeOf(""))
	if err := c.cc.ReadBody(pkg.argv.Interface()); err != nil {
		log.Printf("rpc server: read body failed, err: %v\n", err)
		return nil, err
	}

	return pkg, nil
}

func (c *conn) sendPackage(h *codec.Header, body interface{}, sending *sync.Mutex) {
	sending.Lock()
	defer sending.Unlock()

	if err := c.cc.Write(h, body); err != nil {
		log.Printf("rpc server: write response failed, err: %v\n", err)
	}
}

func (c *conn) handlePackage(pkg *rpcPackage, wg *sync.WaitGroup, sending *sync.Mutex) {
	defer wg.Done()

	log.Println("[server] receive rpcPackage: ", pkg.h, pkg.argv.Elem())
	pkg.replyv = reflect.ValueOf(fmt.Sprintf("covergrpc server resp %d", pkg.h.Seq))
	c.sendPackage(pkg.h, pkg.replyv.Interface(), sending)
}
