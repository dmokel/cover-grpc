package codec

import (
	"bufio"
	"encoding/gob"
	"io"
	"log"
)

// GobCodec ...
type GobCodec struct {
	rwc io.ReadWriteCloser
	buf *bufio.Writer
	dec *gob.Decoder
	enc *gob.Encoder
}

var _ Codec = &GobCodec{}

// NewGobCodec ...
func NewGobCodec(rwc io.ReadWriteCloser) Codec {
	buf := bufio.NewWriter(rwc)
	return &GobCodec{
		rwc: rwc,
		buf: buf,
		dec: gob.NewDecoder(rwc),
		enc: gob.NewEncoder(buf),
	}
}

// ReadHeader ...
func (c *GobCodec) ReadHeader(h *Header) error {
	return c.dec.Decode(h)
}

// ReadBody ...
func (c *GobCodec) ReadBody(body interface{}) error {
	return c.dec.Decode(body)
}

// Write ...
func (c *GobCodec) Write(h *Header, body interface{}) (err error) {
	defer func() {
		_ = c.buf.Flush()
		if err != nil {
			_ = c.Close()
		}
	}()

	if err = c.enc.Encode(h); err != nil {
		log.Printf("gob codec encode header failed, err: %v\n", err)
		return err
	}
	if err = c.enc.Encode(body); err != nil {
		log.Printf("gob codec encode body failed, err: %v\n", err)
		return err
	}

	return nil
}

// Close ...
func (c *GobCodec) Close() error {
	return c.rwc.Close()
}
