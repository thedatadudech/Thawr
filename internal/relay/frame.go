package relay

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Key is a WireGuard public key as carried in frames.
type Key = [32]byte

// Frame types.
const (
	// TypeSend carries a packet from the client to the peer named by Key.
	TypeSend byte = 0x01
	// TypeRecv carries a packet to the client from the peer named by Key.
	TypeRecv byte = 0x02
	// TypePing and TypePong are keepalives; Key is zero.
	TypePing byte = 0x03
	TypePong byte = 0x04
	// TypePeerGone tells the client the destination is not connected or
	// not visible.
	TypePeerGone byte = 0x05
)

// Frame sizes.
const (
	// HeaderLen is type (1) + key (32) + length (2).
	HeaderLen = 35
	// MaxPayload is the largest payload; larger frames close the session.
	MaxPayload = 1500
)

// Frame is one relay message.
type Frame struct {
	Type    byte
	Key     Key
	Payload []byte
}

// Codec errors.
var (
	// ErrOversize means the length field exceeds MaxPayload.
	ErrOversize = errors.New("relay: frame payload exceeds maximum")
	// ErrTruncated means the stream ended inside a frame.
	ErrTruncated = errors.New("relay: truncated frame")
)

// Append encodes f onto buf.
func (f Frame) Append(buf []byte) ([]byte, error) {
	if len(f.Payload) > MaxPayload {
		return nil, fmt.Errorf("%w: %d bytes", ErrOversize, len(f.Payload))
	}
	buf = append(buf, f.Type)
	buf = append(buf, f.Key[:]...)
	buf = binary.BigEndian.AppendUint16(buf, uint16(len(f.Payload))) //nolint:gosec // bounded by MaxPayload
	return append(buf, f.Payload...), nil
}

// WriteFrame writes f to w in one call.
func WriteFrame(w io.Writer, f Frame) error {
	buf, err := f.Append(make([]byte, 0, HeaderLen+len(f.Payload)))
	if err != nil {
		return err
	}
	if _, err := w.Write(buf); err != nil {
		return fmt.Errorf("relay: write frame: %w", err)
	}
	return nil
}

// ReadFrame reads one frame from r. buf must hold at least
// HeaderLen+MaxPayload bytes; the returned payload aliases it.
func ReadFrame(r io.Reader, buf []byte) (Frame, error) {
	if len(buf) < HeaderLen+MaxPayload {
		return Frame{}, errors.New("relay: read buffer too small")
	}
	if _, err := io.ReadFull(r, buf[:HeaderLen]); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return Frame{}, ErrTruncated
		}
		return Frame{}, err
	}
	f := Frame{Type: buf[0]}
	copy(f.Key[:], buf[1:33])
	n := int(binary.BigEndian.Uint16(buf[33:35]))
	if n > MaxPayload {
		return Frame{}, fmt.Errorf("%w: %d bytes", ErrOversize, n)
	}
	if _, err := io.ReadFull(r, buf[HeaderLen:HeaderLen+n]); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return Frame{}, ErrTruncated
		}
		return Frame{}, err
	}
	f.Payload = buf[HeaderLen : HeaderLen+n]
	return f, nil
}

// IsWireGuard reports whether payload starts with a WireGuard message
// type (1-4); the relay carries nothing else.
func IsWireGuard(payload []byte) bool {
	return len(payload) >= 4 && payload[0] >= 1 && payload[0] <= 4
}
