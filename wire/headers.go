package wire

import (
	"encoding/binary"
	"errors"
)

var StaticNames = [...]string{
	"",
	":method",
	":path",
	":status",
	"content-type",
	"content-length",
	"host",
	"user-agent",
	"server",
	"connection",
	"date",
}

var ErrMalformed = errors.New("malformed header block")

type Header struct {
	Name  string
	Value string
}

func Get(hs []Header, name string) (string, bool) {
	for _, h := range hs {
		if h.Name == name {
			return h.Value, true
		}
	}
	return "", false
}

func staticIndex(name string) int {
	for i := 1; i < len(StaticNames); i++ {
		if StaticNames[i] == name {
			return i
		}
	}
	return 0
}

func EncodeHeaders(hs []Header) ([]byte, error) {
	var b []byte
	for _, h := range hs {
		if len(h.Value) > 0xFFFF {
			return nil, ErrMalformed
		}
		if i := staticIndex(h.Name); i > 0 {
			b = append(b, byte(i))
		} else {
			if len(h.Name) == 0 || len(h.Name) > 0xFF {
				return nil, ErrMalformed
			}
			b = append(b, 0, byte(len(h.Name)))
			b = append(b, h.Name...)
		}
		b = binary.BigEndian.AppendUint16(b, uint16(len(h.Value)))
		b = append(b, h.Value...)
	}
	return b, nil
}

func DecodeHeaders(b []byte) ([]Header, error) {
	var hs []Header
	for len(b) > 0 {
		var h Header
		tag := b[0]
		b = b[1:]
		switch {
		case tag == 0:
			if len(b) < 1 {
				return nil, ErrMalformed
			}
			n := int(b[0])
			b = b[1:]
			if n == 0 || len(b) < n {
				return nil, ErrMalformed
			}
			h.Name = string(b[:n])
			b = b[n:]
		case int(tag) < len(StaticNames):
			h.Name = StaticNames[tag]
		default:
			return nil, ErrMalformed
		}
		if len(b) < 2 {
			return nil, ErrMalformed
		}
		n := int(binary.BigEndian.Uint16(b))
		b = b[2:]
		if len(b) < n {
			return nil, ErrMalformed
		}
		h.Value = string(b[:n])
		b = b[n:]
		hs = append(hs, h)
	}
	return hs, nil
}
