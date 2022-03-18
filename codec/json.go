package codec

import (
	"bufio"
	"encoding/json"
	"io"
	"log"
)

// JSONCodec ...
type JSONCodec struct {
	rwc io.ReadWriteCloser
	buf *bufio.Writer
	dec *json.Decoder
	enc *json.Encoder
}

// NewJSONCodec ...
func NewJSONCodec(rwc io.ReadWriteCloser) Codec {
	buf := bufio.NewWriter(rwc)
	return &JSONCodec{
		rwc: rwc,
		buf: buf,
		dec: json.NewDecoder(rwc),
		enc: json.NewEncoder(buf),
	}
}

// ReadHeader ...
func (c *JSONCodec) ReadHeader(h *Header) error {
	return c.dec.Decode(h)
}

// ReadBody ...
func (c *JSONCodec) ReadBody(body interface{}) error {
	return c.enc.Encode(body)
}

// Write ...
func (c *JSONCodec) Write(h *Header, body interface{}) (err error) {
	defer func() {
		_ = c.buf.Flush()
		if err != nil {
			c.Close()
		}
	}()

	if err = c.enc.Encode(h); err != nil {
		log.Printf("json codec encode header failed, err: %v\n", err)
		return err
	}
	if err = c.enc.Encode(body); err != nil {
		log.Printf("json codec encode body failed, err: %v\n", err)
		return err
	}

	return nil
}

// Close ...
func (c *JSONCodec) Close() error {
	return c.rwc.Close()
}
