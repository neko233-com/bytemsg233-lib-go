package bytemsg233

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

type WireType uint8

const (
	WireVarint WireType = 0
	WireBytes  WireType = 2
)

type Resettable interface {
	Reset()
}

type Pool[T Resettable] struct {
	items   []T
	factory func() T
}

func NewPool[T Resettable](factory func() T) *Pool[T] {
	return &Pool[T]{
		items:   make([]T, 0),
		factory: factory,
	}
}

func (p *Pool[T]) Acquire() T {
	if n := len(p.items); n > 0 {
		value := p.items[n-1]
		p.items = p.items[:n-1]
		return value
	}
	return p.factory()
}

func (p *Pool[T]) Release(value T) {
	value.Reset()
	p.items = append(p.items, value)
}

type Writer struct {
	buf bytes.Buffer
}

func NewWriter() *Writer {
	return &Writer{}
}

func (w *Writer) Bytes() []byte {
	return w.buf.Bytes()
}

func (w *Writer) WriteHeader(tag uint32, wireType WireType) {
	w.WriteVarint(uint64(tag<<3 | uint32(wireType)))
}

func (w *Writer) WriteVarint(value uint64) {
	var tmp [10]byte
	n := binary.PutUvarint(tmp[:], value)
	w.buf.Write(tmp[:n])
}

func (w *Writer) WriteString(tag uint32, value string) {
	w.WriteHeader(tag, WireBytes)
	w.WriteVarint(uint64(len(value)))
	w.buf.WriteString(value)
}

type Reader struct {
	r *bytes.Reader
}

func NewReader(data []byte) *Reader {
	return &Reader{r: bytes.NewReader(data)}
}

func (r *Reader) EOF() bool {
	return r.r.Len() == 0
}

func (r *Reader) ReadHeader() (uint32, WireType, error) {
	value, err := binary.ReadUvarint(r.r)
	if err != nil {
		return 0, 0, err
	}
	return uint32(value >> 3), WireType(value & 0x7), nil
}

func (r *Reader) ReadVarint() (uint64, error) {
	return binary.ReadUvarint(r.r)
}

func (r *Reader) ReadString() (string, error) {
	n, err := binary.ReadUvarint(r.r)
	if err != nil {
		return "", err
	}
	if n > uint64(r.r.Len()) {
		return "", io.ErrUnexpectedEOF
	}
	data := make([]byte, n)
	if _, err := io.ReadFull(r.r, data); err != nil {
		return "", err
	}
	return string(data), nil
}

func EnumFromValue[T ~int32](value int32, ok func(T) bool) (T, error) {
	candidate := T(value)
	if ok(candidate) {
		return candidate, nil
	}
	return candidate, fmt.Errorf("unknown enum value: %d", value)
}
