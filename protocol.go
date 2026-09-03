package main

import (
	"encoding/binary"
	"errors"
	"io"
)

// Version is the wire protocol version.
const Version byte = 1

// Frame layout: [type:1][len:2 LE][payload:len]
const (
	PktHello   byte = 0x01 // client -> server: HelloMsg JSON
	PktWelcome byte = 0x02 // server -> client: WelcomeMsg JSON
	PktDenied  byte = 0x03 // server -> client: reason string
	PktPing    byte = 0x04 // either direction, empty
	PktPong    byte = 0x05 // reply to ping, empty
	PktOpen    byte = 0x06 // server -> client: uint32 streamID + host hint
	PktData    byte = 0x07 // either: uint32 streamID + payload
	PktClose   byte = 0x08 // either: uint32 streamID
)

// MaxPayload guards a single Data frame size.
const MaxPayload = 256 * 1024

var ErrFrameTooLarge = errors.New("dt: frame too large")

// ReadFrame reads one frame. payload buffer is freshly allocated.
func ReadFrame(r io.Reader) (typ byte, payload []byte, err error) {
	var hdr [3]byte
	if _, err = io.ReadFull(r, hdr[:]); err != nil {
		return
	}
	typ = hdr[0]
	n := binary.LittleEndian.Uint16(hdr[1:3])
	if int(n) > MaxPayload {
		return typ, nil, ErrFrameTooLarge
	}
	buf := make([]byte, n)
	if _, err = io.ReadFull(r, buf); err != nil {
		return
	}
	return typ, buf, nil
}

// WriteFrame writes one frame of type typ with payload to w.
func WriteFrame(w io.Writer, typ byte, payload []byte) error {
	var hdr [3]byte
	hdr[0] = typ
	binary.LittleEndian.PutUint16(hdr[1:3], uint16(len(payload)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	if len(payload) > 0 {
		if _, err := w.Write(payload); err != nil {
			return err
		}
	}
	return nil
}

// PutU32 stores v little-endian into a 4-byte slice (must be len>=4).
func PutU32(buf []byte, v uint32) { binary.LittleEndian.PutUint32(buf, v) }

// GetU32 reads a little-endian uint32 from a 4-byte slice.
func GetU32(buf []byte) uint32 { return binary.LittleEndian.Uint32(buf) }

// u32b returns v as a fresh 4-byte little-endian slice.
func u32b(v uint32) []byte {
	b := make([]byte, 4)
	PutU32(b, v)
	return b
}

// StreamID extracts the stream id from an Open/Data/Close payload.
func StreamID(payload []byte) uint32 { return binary.LittleEndian.Uint32(payload[:4]) }
