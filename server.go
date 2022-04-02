package covergrpc

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"io"
	"log"
	"net"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dmokel/cover-grpc/codec"
)

// MagicNumber ...
// define the unique magic number for cover-grpc
const magicNumber = 0x3bef5c

// Option ...
type Option struct {
	MargicNumber      int        // MagicNumber marks this's a cover-grpc rpcPackage
	CodecType         codec.Type // client may choose different Codec to encode body
	ConnectionTimeout time.Duration
	HandleTimeout     time.Duration
}

// DefaultOption ...
var DefaultOption = &defaultOption
var defaultOption = Option{
	MargicNumber:      magicNumber,
	CodecType:         codec.GobType,
	ConnectionTimeout: time.Second * 10,
}

// Server ...
type Server struct {
	serviceMap sync.Map
}

// NewServer ...
func NewServer() *Server {
	return &Server{}
}

// Register publishes in the server the set of methods of the
func (s *Server) Register(rcvr interface{}) error {
	service := newService(rcvr)
	if _, dup := s.serviceMap.LoadOrStore(service.name, service); dup {
		return errors.New("rpc server: service already defined: " + service.name)
	}
	return nil
}

// Register publishes in the server the set of methods of the
func Register(rcvr interface{}) error { return defaultServer.Register(rcvr) }

func (s *Server) findService(serviceMethod string) (svc *service, mtype *methodType, err error) {
	dot := strings.LastIndex(serviceMethod, ".")
	if dot < 0 {
		err = errors.New("rpc server: service/method request ill-formed: " + serviceMethod)
		return
	}
	serviceName, methodName := serviceMethod[:dot], serviceMethod[dot+1:]
	svci, ok := s.serviceMap.Load(serviceName)
	if !ok {
		err = errors.New("rpc server: can't find service " + serviceName)
		return
	}
	svc = svci.(*service)
	mtype = svc.methods[methodName]
	if mtype == nil {
		err = errors.New("rpc server: can't find method " + methodName)
	}
	return
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
		srv:           s,
		cc:            cc,
		handleTimeout: opt.HandleTimeout,
	}
}

// conn represent the rpc connection
type conn struct {
	srv    *Server
	client *Client
	cc     codec.Codec

	handleTimeout time.Duration
}

// rpcPackage stores all information of a call
type rpcPackage struct {
	conn         *conn
	h            *codec.Header // header of rpcPackage
	argv, replyv reflect.Value // argv and replyv of rpcPackage
	mtype        *methodType   // method type of rpcPackage
	svc          *service      // service of rpcPackage
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
	var err error
	if err = c.cc.ReadHeader(&h); err != nil {
		if err != io.EOF && err != io.ErrUnexpectedEOF {
			log.Printf("rpc server: read header failed, err: %v\n", err)
		}
		return nil, err
	}

	pkg := &rpcPackage{
		conn: c,
		h:    &h,
	}
	pkg.svc, pkg.mtype, err = c.srv.findService(h.ServiceMethod)
	if err != nil {
		return nil, err
	}

	pkg.argv = pkg.mtype.newArgv()
	pkg.replyv = pkg.mtype.newReplyv()

	// make sure that argvi is a pointer, ReadBody need a pointer as parameter
	argvi := pkg.argv.Interface()
	if pkg.argv.Kind() != reflect.Ptr {
		argvi = pkg.argv.Addr().Interface()
	}
	if err = c.cc.ReadBody(argvi); err != nil {
		log.Printf("rpc server: read rpcPackage body failed, err: %v\n", err)
		return nil, err
	}

	return pkg, nil
}

func (c *conn) handlePackage(pkg *rpcPackage, wg *sync.WaitGroup, sending *sync.Mutex) {
	defer wg.Done()

	called := make(chan struct{})
	sent := make(chan struct{})
	go func() {
		err := pkg.svc.call(pkg.mtype, pkg.argv, pkg.replyv)
		called <- struct{}{}
		if err != nil {
			pkg.h.Error = err.Error()
			c.sendPackage(pkg.h, invalidPackage, sending)
			sent <- struct{}{}
			return
		}
		c.sendPackage(pkg.h, pkg.replyv.Interface(), sending)
		sent <- struct{}{}
	}()

	if c.handleTimeout == 0 {
		<-called
		<-sent
		return
	}
	select {
	case <-time.After(c.handleTimeout):
		pkg.h.Error = fmt.Sprintf("rpc server: rpcPackage handle timeout: expect within %s", c.handleTimeout)
		c.sendPackage(pkg.h, invalidPackage, sending)
	case <-called:
		<-sent
	}
}

func (c *conn) sendPackage(h *codec.Header, body interface{}, sending *sync.Mutex) {
	sending.Lock()
	defer sending.Unlock()

	if err := c.cc.Write(h, body); err != nil {
		log.Printf("rpc server: write response failed, err: %v\n", err)
	}
}

func isExportedOrBuiltinType(t reflect.Type) bool {
	return ast.IsExported(t.Name()) || t.PkgPath() == ""
}

type methodType struct {
	method    reflect.Method
	ArgType   reflect.Type
	ReplyType reflect.Type
	numCalls  uint64
}

func (m *methodType) NumCalls() uint64 {
	return atomic.LoadUint64(&m.numCalls)
}

func (m *methodType) newArgv() reflect.Value {
	var argv reflect.Value
	// arg may be a pointer type, or a value type
	if m.ArgType.Kind() == reflect.Ptr {
		argv = reflect.New(m.ArgType.Elem())
	} else {
		argv = reflect.New(m.ArgType).Elem()
	}
	return argv
}

func (m *methodType) newReplyv() reflect.Value {
	// reply must be a pointer type
	replyv := reflect.New(m.ReplyType.Elem())
	switch m.ReplyType.Elem().Kind() {
	case reflect.Map:
		replyv.Elem().Set(reflect.MakeMap(m.ReplyType.Elem()))
	case reflect.Slice:
		replyv.Elem().Set(reflect.MakeSlice(m.ReplyType.Elem(), 0, 0))
	}
	return replyv
}

type service struct {
	name    string
	typ     reflect.Type
	rcvr    reflect.Value
	methods map[string]*methodType
}

func newService(rcvr interface{}) *service {
	s := new(service)
	s.rcvr = reflect.ValueOf(rcvr)
	s.name = reflect.Indirect(s.rcvr).Type().Name()
	s.typ = reflect.TypeOf(rcvr)
	if !ast.IsExported(s.name) {
		log.Fatalf("rpc server: %s is not a valid service name", s.name)
	}
	s.registerMethod()
	return s
}

func (s *service) registerMethod() {
	s.methods = make(map[string]*methodType)
	for i := 0; i < s.typ.NumMethod(); i++ {
		method := s.typ.Method(i)
		mType := method.Type
		if mType.NumIn() != 3 || mType.NumOut() != 1 {
			continue
		}
		if mType.Out(0) != reflect.TypeOf((*error)(nil)).Elem() {
			continue
		}
		argType, replyType := mType.In(1), mType.In(2)
		if !isExportedOrBuiltinType(argType) || !isExportedOrBuiltinType(replyType) {
			continue
		}
		s.methods[method.Name] = &methodType{
			method:    method,
			ArgType:   argType,
			ReplyType: replyType,
		}
		log.Printf("rpc server: register %s.%s\n", s.name, method.Name)
	}
}

func (s *service) call(m *methodType, argv, replyv reflect.Value) error {
	atomic.AddUint64(&m.numCalls, 1)
	f := m.method.Func
	returnValues := f.Call([]reflect.Value{s.rcvr, argv, replyv})
	if errInter := returnValues[0].Interface(); errInter != nil {
		return errInter.(error)
	}
	return nil
}
