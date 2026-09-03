package relay

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"testing"
)

func TestFrameCodec(t *testing.T) {
	var key Key
	_, _ = rand.Read(key[:])
	payload := make([]byte, 1500)
	_, _ = rand.Read(payload)
	payload[0] = 4
	cases := []Frame{
		{Type: TypeSend, Key: key, Payload: payload},
		{Type: TypeRecv, Key: key, Payload: []byte{1, 0, 0, 0}},
		{Type: TypePing},
		{Type: TypePong},
		{Type: TypePeerGone, Key: key},
	}
	buf := make([]byte, HeaderLen+MaxPayload)
	for _, want := range cases {
		var b bytes.Buffer
		if err := WriteFrame(&b, want); err != nil {
			t.Fatal(err)
		}
		if b.Len() != HeaderLen+len(want.Payload) {
			t.Errorf("type %d: encoded %d bytes", want.Type, b.Len())
		}
		got, err := ReadFrame(&b, buf)
		if err != nil {
			t.Fatalf("type %d: %v", want.Type, err)
		}
		if got.Type != want.Type || got.Key != want.Key || !bytes.Equal(got.Payload, want.Payload) {
			t.Errorf("type %d: round trip mismatch", want.Type)
		}
	}
	// Oversize on write and on read.
	if err := WriteFrame(io.Discard, Frame{Type: TypeSend, Payload: make([]byte, MaxPayload+1)}); !errors.Is(err, ErrOversize) {
		t.Errorf("oversize write: %v", err)
	}
	hdr := make([]byte, HeaderLen)
	hdr[0], hdr[33], hdr[34] = TypeSend, 0xff, 0xff
	if _, err := ReadFrame(bytes.NewReader(hdr), buf); !errors.Is(err, ErrOversize) {
		t.Errorf("oversize read: %v", err)
	}
	// Truncated header and truncated payload.
	if _, err := ReadFrame(bytes.NewReader(hdr[:10]), buf); !errors.Is(err, ErrTruncated) {
		t.Errorf("truncated header: %v", err)
	}
	var b bytes.Buffer
	_ = WriteFrame(&b, Frame{Type: TypeSend, Payload: []byte{1, 2, 3, 4, 5}})
	if _, err := ReadFrame(bytes.NewReader(b.Bytes()[:b.Len()-2]), buf); !errors.Is(err, ErrTruncated) {
		t.Errorf("truncated payload: %v", err)
	}
	if _, err := ReadFrame(bytes.NewReader(nil), buf); !errors.Is(err, io.EOF) {
		t.Errorf("clean EOF: %v", err)
	}
	if IsWireGuard([]byte{0, 0, 0, 0}) || IsWireGuard([]byte{5, 0, 0, 0}) || IsWireGuard([]byte{1}) || !IsWireGuard([]byte{1, 0, 0, 0}) {
		t.Error("IsWireGuard")
	}
}
