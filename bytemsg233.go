package bytemsg233

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
	"unsafe"
)

type WireType uint8

const (
	WireVarint  WireType = 0
	WireFixed64 WireType = 1
	WireBytes   WireType = 2
	WireFixed32 WireType = 5
)

type BlockKind uint8

const (
	BlockPackedVarint BlockKind = 1
	BlockPackedZigzag BlockKind = 2
	BlockDeltaVarint  BlockKind = 3
	BlockBoolBitset   BlockKind = 4
	BlockStringList   BlockKind = 5
	BlockColumnList   BlockKind = 6
)

var errVarintOverflow = errors.New("bytemsg233: varint overflows a 64-bit integer")

type Resettable interface {
	Reset()
}

type Pool[T Resettable] struct {
	items sync.Pool
}

func NewPool[T Resettable](factory func() T) *Pool[T] {
	if factory == nil {
		panic("bytemsg233: pool factory is nil")
	}
	return &Pool[T]{
		items: sync.Pool{
			New: func() any {
				return factory()
			},
		},
	}
}

func (p *Pool[T]) Acquire() T {
	if p == nil {
		var zero T
		return zero
	}
	value, ok := p.items.Get().(T)
	if ok {
		return value
	}
	var zero T
	return zero
}

func (p *Pool[T]) Release(value T) {
	if p == nil {
		return
	}
	value.Reset()
	p.items.Put(value)
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
	w.WriteStringValue(value)
}

func (w *Writer) WriteStringValue(value string) {
	w.WriteVarint(uint64(len(value)))
	w.buf = append(w.buf, value...)
}

func (w *Writer) WriteBytes(value []byte) {
	w.WriteVarint(uint64(len(value)))
	w.buf = append(w.buf, value...)
}

func (w *Writer) WriteFixed32(value uint32) {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], value)
	w.buf = append(w.buf, buf[:]...)
}

func (w *Writer) WriteFixed64(value uint64) {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], value)
	w.buf = append(w.buf, buf[:]...)
}

func (w *Writer) WritePackedVarints(values []uint64) {
	w.WriteVarint(uint64(len(values)))
	for _, value := range values {
		w.WriteVarint(value)
	}
}

func (w *Writer) WriteDeltaVarints(values []uint64) {
	w.WriteVarint(uint64(len(values)))
	if len(values) == 0 {
		return
	}
	prev := values[0]
	w.WriteVarint(prev)
	for _, value := range values[1:] {
		w.WriteVarint(ZigZagEncode(int64(value) - int64(prev)))
		prev = value
	}
}

func (w *Writer) WriteBoolBitset(values []bool) {
	w.WriteVarint(uint64(len(values)))
	var current byte
	for i, value := range values {
		if value {
			current |= 1 << uint(i&7)
		}
		if i&7 == 7 {
			w.buf = append(w.buf, current)
			current = 0
		}
	}
	if len(values)&7 != 0 {
		w.buf = append(w.buf, current)
	}
}

func (w *Writer) WriteStringList(values []string) {
	w.WriteVarint(uint64(len(values)))
	for _, value := range values {
		w.WriteVarint(uint64(len(value)))
		w.buf = append(w.buf, value...)
	}
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

func (r *Reader) ReadBytesView() ([]byte, error) {
	n, err := r.ReadVarint()
	if err != nil {
		return nil, err
	}
	if n > uint64(len(r.data)-r.pos) {
		return nil, io.ErrUnexpectedEOF
	}
	start := r.pos
	r.pos += int(n)
	return r.data[start:r.pos], nil
}

func (r *Reader) ReadFixed32() (uint32, error) {
	if len(r.data)-r.pos < 4 {
		return 0, io.ErrUnexpectedEOF
	}
	value := binary.LittleEndian.Uint32(r.data[r.pos:])
	r.pos += 4
	return value, nil
}

func (r *Reader) ReadFixed64() (uint64, error) {
	if len(r.data)-r.pos < 8 {
		return 0, io.ErrUnexpectedEOF
	}
	value := binary.LittleEndian.Uint64(r.data[r.pos:])
	r.pos += 8
	return value, nil
}

func (r *Reader) SkipField(wireType WireType) error {
	switch wireType {
	case WireVarint:
		_, err := r.ReadVarint()
		return err
	case WireFixed64:
		_, err := r.ReadFixed64()
		return err
	case WireBytes:
		_, err := r.ReadBytesView()
		return err
	case WireFixed32:
		_, err := r.ReadFixed32()
		return err
	default:
		return errors.New("bytemsg233: unsupported wire type")
	}
}

func (r *Reader) ReadPackedVarints(dst []uint64) ([]uint64, error) {
	count, err := r.ReadVarint()
	if err != nil {
		return dst, err
	}
	if uint64(cap(dst)) < count {
		dst = make([]uint64, 0, int(count))
	} else {
		dst = dst[:0]
	}
	for i := uint64(0); i < count; i++ {
		value, err := r.ReadVarint()
		if err != nil {
			return dst, err
		}
		dst = append(dst, value)
	}
	return dst, nil
}

func (r *Reader) ReadDeltaVarints(dst []uint64) ([]uint64, error) {
	count, err := r.ReadVarint()
	if err != nil {
		return dst, err
	}
	if uint64(cap(dst)) < count {
		dst = make([]uint64, 0, int(count))
	} else {
		dst = dst[:0]
	}
	if count == 0 {
		return dst, nil
	}
	value, err := r.ReadVarint()
	if err != nil {
		return dst, err
	}
	dst = append(dst, value)
	for i := uint64(1); i < count; i++ {
		deltaRaw, err := r.ReadVarint()
		if err != nil {
			return dst, err
		}
		value = uint64(int64(value) + ZigZagDecode(deltaRaw))
		dst = append(dst, value)
	}
	return dst, nil
}

func (r *Reader) ReadBoolBitset(dst []bool) ([]bool, error) {
	count, err := r.ReadVarint()
	if err != nil {
		return dst, err
	}
	if uint64(cap(dst)) < count {
		dst = make([]bool, int(count))
	} else {
		dst = dst[:int(count)]
		for i := range dst {
			dst[i] = false
		}
	}
	for i := uint64(0); i < count; i += 8 {
		if r.pos >= len(r.data) {
			return dst, io.ErrUnexpectedEOF
		}
		current := r.data[r.pos]
		r.pos++
		limit := uint64(8)
		if remaining := count - i; remaining < limit {
			limit = remaining
		}
		for bit := uint64(0); bit < limit; bit++ {
			dst[int(i+bit)] = current&(1<<bit) != 0
		}
	}
	return dst, nil
}

func (r *Reader) ReadStringList(dst []string) ([]string, error) {
	count, err := r.ReadVarint()
	if err != nil {
		return dst, err
	}
	if uint64(cap(dst)) < count {
		dst = make([]string, 0, int(count))
	} else {
		dst = dst[:0]
	}
	for i := uint64(0); i < count; i++ {
		value, err := r.ReadStringView()
		if err != nil {
			return dst, err
		}
		dst = append(dst, value)
	}
	return dst, nil
}

func EnumFromValue[T ~int32](value int32, ok func(T) bool) (T, error) {
	candidate := T(value)
	if ok(candidate) {
		return candidate, nil
	}
	return candidate, fmt.Errorf("unknown enum value: %d", value)
}

func ZigZagEncode(value int64) uint64 {
	return uint64((value << 1) ^ (value >> 63))
}

func ZigZagDecode(value uint64) int64 {
	return int64((value >> 1) ^ -(value & 1))
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
