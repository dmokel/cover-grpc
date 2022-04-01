package covergrpc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sync"

	"github.com/dmokel/cover-grpc/codec"
)

// Call represents an active RPC.
type Call struct {
	Seq           uint64
	ServiceMethod string      // format "<service>.<method>"
	Args          interface{} // arguments to the function
	Reply         interface{} // reply from the function
	Error         error       // if error occurs, it will be set
	Done          chan *Call  // Strobes when call is complete.
}

func (call *Call) done() {
	call.Done <- call
}

// Client represents an RPC Client.
// There may be multiple outstanding Calls associated
// with a single Client, and a Client may be used by
// multiple goroutines simultaneously.
type Client struct {
	conn     *conn
	opt      *Option
	sending  sync.Mutex // protect following
	header   codec.Header
	mu       sync.RWMutex // protect following
	seq      uint64
	pending  map[uint64]*Call
	closing  bool // user has called Close
	shutdown bool // server has told us to stop
}

var _ io.Closer = &Client{}

// Close the connection
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closing {
		return ErrShutdown
	}
	c.closing = true
	return c.conn.close()
}

// IsAvailable return true if the client does work
func (c *Client) IsAvailable() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return !c.shutdown && !c.closing
}

// ErrShutdown ...
var ErrShutdown = errors.New("connection is closed")

// NewClient ...
func NewClient(opt *Option) *Client {
	c := &Client{
		opt:     opt,
		seq:     1, // seq starts with 1, 0 means invalid call
		pending: make(map[uint64]*Call),
	}
	return c
}

// Dial to rpc server and receive reply from the server circularly
func (c *Client) Dial(network, address string) (err error) {
	var rwc net.Conn
	defer func() {
		if err != nil {
			_ = rwc.Close()
		}
	}()

	f := codec.NewCodecFuncMap[c.opt.CodecType]
	if f == nil {
		err = fmt.Errorf("invalid codec type %s", c.opt.CodecType)
		log.Println("rpc client: codec error:", err)
		return
	}

	rwc, err = net.Dial(network, address)
	if err != nil {
		log.Println("rpc client: dial error:", err)
		return
	}

	if err = json.NewEncoder(rwc).Encode(c.opt); err != nil {
		log.Println("rpc client: option encode failed, err: ", err)
		return
	}

	cc := f(rwc)
	conn := c.newConn(cc)
	c.conn = conn
	go conn.receive()
	return
}

func (c *Client) newConn(cc codec.Codec) *conn {
	return &conn{
		client: c,
		cc:     cc,
	}
}

// receive reply from the server circularly
func (c *conn) receive() {
	var err error
	for err == nil {
		var h codec.Header
		if err = c.cc.ReadHeader(&h); err != nil {
			break
		}
		call := c.client.removeCall(h.Seq)
		switch {
		case call == nil:
			err = c.cc.ReadBody(nil)
		case h.Error != "":
			call.Error = fmt.Errorf(h.Error)
			err = c.cc.ReadBody(nil)
			call.done()
		default:
			err = c.cc.ReadBody(call.Reply)
			if err != nil {
				call.Error = errors.New("read body failed, err:" + err.Error())
			}
			call.done()
		}
	}
	c.client.terminateCalls(err)
}

func (c *Client) registerCall(call *Call) (uint64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.shutdown || c.closing {
		return 0, ErrShutdown
	}

	call.Seq = c.seq
	c.pending[call.Seq] = call
	c.seq++
	return call.Seq, nil
}

// removeCall removes a pending Call from the map and return the Call
func (c *Client) removeCall(seq uint64) *Call {
	c.mu.Lock()
	defer c.mu.Unlock()
	call := c.pending[seq]
	delete(c.pending, seq)
	return call
}

func (c *Client) terminateCalls(err error) {
	c.sending.Lock() // TODO why sending lock
	defer c.sending.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.shutdown = true
	for _, call := range c.pending {
		call.Error = err
		call.done()
	}
}

func parseOption(opts ...*Option) (*Option, error) {
	if len(opts) == 0 || opts[0] == nil {
		return DefaultOption, nil
	}
	if len(opts) != 1 {
		return nil, errors.New("number of options is more than 1")
	}
	opt := opts[0]
	opt.MargicNumber = defaultOption.MargicNumber
	if opt.CodecType == "" {
		opt.CodecType = defaultOption.CodecType
	}
	return opt, nil
}

// Dial connects to an RPC server at the specified network address
func Dial(network, address string, opts ...*Option) (c *Client, err error) {
	opt, err := parseOption(opts...)
	if err != nil {
		return nil, err
	}

	client := NewClient(opt)
	err = client.Dial(network, address)
	if err != nil {
		return nil, err
	}

	return client, nil
}

func (c *Client) send(call *Call) {
	c.sending.Lock()
	defer c.sending.Unlock()

	seq, err := c.registerCall(call)
	if err != nil {
		call.Error = err
		call.done()
		return
	}

	c.header.ServiceMethod = call.ServiceMethod
	c.header.Seq = seq
	c.header.Error = ""

	if err := c.conn.cc.Write(&c.header, call.Args); err != nil {
		call := c.removeCall(seq)
		if call != nil {
			call.Error = err
			call.done()
		}
	}
}

// Go invokes the function asynchronously.
// It returns the Call structure representing the invocation.
func (c *Client) Go(serviceMethod string, args, reply interface{}, done chan *Call) *Call {
	if done == nil {
		done = make(chan *Call, 10)
	} else if cap(done) == 0 {
		log.Panic("rpc: done channel is unbuffered")
	}

	call := &Call{
		ServiceMethod: serviceMethod,
		Args:          args,
		Reply:         reply,
		Done:          done,
	}
	go c.send(call)
	return call
}

// Call invokes the named function, waits for it to complete,
// and returns its error status.
func (c *Client) Call(serverMethod string, args, reply interface{}) error {
	call := <-c.Go(serverMethod, args, reply, make(chan *Call, 1)).Done
	return call.Error
}
