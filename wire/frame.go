package wire

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	HeaderSize    = 8
	MaxPayload    = 1 << 24
	TypeHeaders   = 0x01
	TypeData      = 0x02
	FlagEndStream = 0x01
)

var ErrTooLarge = errors.New("frame payload too large")

type Frame struct {
	Type     uint8
	Flags    uint8
	StreamID uint16
	Payload  []byte
}

func (f Frame) EndStream() bool {
	return f.Flags&FlagEndStream != 0
}

func (f Frame) Bytes() []byte {
	b := make([]byte, HeaderSize+len(f.Payload))
	binary.BigEndian.PutUint32(b[0:4], uint32(len(f.Payload)))
	b[4] = f.Type
	b[5] = f.Flags
	binary.BigEndian.PutUint16(b[6:8], f.StreamID)
	copy(b[HeaderSize:], f.Payload)
	return b
}

func (f Frame) String() string {
	return fmt.Sprintf("%s stream=%d flags=0x%02x length=%d", TypeName(f.Type), f.StreamID, f.Flags, len(f.Payload))
}

func TypeName(t uint8) string {
	switch t {
	case TypeHeaders:
		return "HEADERS"
	case TypeData:
		return "DATA"
	}
	return fmt.Sprintf("UNKNOWN(0x%02x)", t)
}

func WriteFrame(w io.Writer, f Frame) error {
	if len(f.Payload) > MaxPayload {
		return ErrTooLarge
	}
	_, err := w.Write(f.Bytes())
	return err
}

func ReadFrame(r io.Reader) (Frame, error) {
	var h [HeaderSize]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return Frame{}, err
	}
	n := binary.BigEndian.Uint32(h[0:4])
	f := Frame{Type: h[4], Flags: h[5], StreamID: binary.BigEndian.Uint16(h[6:8])}
	if n > MaxPayload {
		if _, err := io.CopyN(io.Discard, r, int64(n)); err != nil {
			return f, err
		}
		return f, ErrTooLarge
	}
	f.Payload = make([]byte, n)
	if _, err := io.ReadFull(r, f.Payload); err != nil {
		return f, err
	}
	return f, nil
}
