package bytemsg233

import (
	"errors"
	"fmt"
	"io"
	"unsafe"
)

type WireType uint8

const (
	WireVarint WireType = 0
	WireBytes  WireType = 2
)

var errVarintOverflow = errors.New("bytemsg233: varint overflows a 64-bit integer")

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
	buf []byte
}

func NewWriter() *Writer {
	return &Writer{}
}

func NewWriterSize(size int) *Writer {
	return &Writer{buf: make([]byte, 0, size)}
}

func (w *Writer) Bytes() []byte {
	return w.buf
}

func (w *Writer) Reset() {
	w.buf = w.buf[:0]
}

func (w *Writer) WriteHeader(tag uint32, wireType WireType) {
	w.WriteVarint(uint64(tag<<3 | uint32(wireType)))
}

func (w *Writer) WriteVarint(value uint64) {
	for value >= 0x80 {
		w.buf = append(w.buf, byte(value)|0x80)
		value >>= 7
	}
	w.buf = append(w.buf, byte(value))
}

func (w *Writer) WriteString(tag uint32, value string) {
	w.WriteHeader(tag, WireBytes)
	w.WriteVarint(uint64(len(value)))
	w.buf = append(w.buf, value...)
}

type Reader struct {
	data []byte
	pos  int
}

func NewReader(data []byte) *Reader {
	return &Reader{data: data}
}

func (r *Reader) Reset(data []byte) {
	r.data = data
	r.pos = 0
}

func (r *Reader) EOF() bool {
	return r.pos >= len(r.data)
}

func (r *Reader) Remaining() int {
	return len(r.data) - r.pos
}

func (r *Reader) ReadHeader() (uint32, WireType, error) {
	if r.pos >= len(r.data) {
		return 0, 0, io.ErrUnexpectedEOF
	}
	b := r.data[r.pos]
	var value uint64
	if b < 0x80 {
		r.pos++
		value = uint64(b)
	} else {
		var err error
		value, err = r.readVarintSlow()
		if err != nil {
			return 0, 0, err
		}
	}
	return uint32(value >> 3), WireType(value & 0x7), nil
}

func (r *Reader) ReadVarint() (uint64, error) {
	if r.pos >= len(r.data) {
		return 0, io.ErrUnexpectedEOF
	}
	b := r.data[r.pos]
	if b < 0x80 {
		r.pos++
		return uint64(b), nil
	}
	return r.readVarintSlow()
}

func (r *Reader) ReadString() (string, error) {
	if r.pos >= len(r.data) {
		return "", io.ErrUnexpectedEOF
	}
	b := r.data[r.pos]
	var n uint64
	if b < 0x80 {
		r.pos++
		n = uint64(b)
	} else {
		var err error
		n, err = r.readVarintSlow()
		if err != nil {
			return "", err
		}
	}
	if n > uint64(len(r.data)-r.pos) {
		return "", io.ErrUnexpectedEOF
	}
	start := r.pos
	r.pos += int(n)
	return string(r.data[start:r.pos]), nil
}

// ReadStringView reads a length-prefixed string without copying.
//
// The returned string aliases the reader input. Use it only when the input
// byte slice will stay alive and immutable for at least as long as the string.
func (r *Reader) ReadStringView() (string, error) {
	if r.pos >= len(r.data) {
		return "", io.ErrUnexpectedEOF
	}
	b := r.data[r.pos]
	var n uint64
	if b < 0x80 {
		r.pos++
		n = uint64(b)
	} else {
		var err error
		n, err = r.readVarintSlow()
		if err != nil {
			return "", err
		}
	}
	if n > uint64(len(r.data)-r.pos) {
		return "", io.ErrUnexpectedEOF
	}
	start := r.pos
	r.pos += int(n)
	if n == 0 {
		return "", nil
	}
	value := r.data[start:r.pos]
	return unsafe.String(unsafe.SliceData(value), len(value)), nil
}

func EnumFromValue[T ~int32](value int32, ok func(T) bool) (T, error) {
	candidate := T(value)
	if ok(candidate) {
		return candidate, nil
	}
	return candidate, fmt.Errorf("unknown enum value: %d", value)
}

func (r *Reader) readVarintSlow() (uint64, error) {
	var value uint64
	for shift := uint(0); shift < 64; shift += 7 {
		if r.pos >= len(r.data) {
			return 0, io.ErrUnexpectedEOF
		}
		b := r.data[r.pos]
		r.pos++
		if b < 0x80 {
			if shift == 63 && b > 1 {
				return 0, errVarintOverflow
			}
			return value | uint64(b)<<shift, nil
		}
		value |= uint64(b&0x7f) << shift
	}
	return 0, errVarintOverflow
}
