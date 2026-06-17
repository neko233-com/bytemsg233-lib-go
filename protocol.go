package bytemsg233

import "errors"

var ErrProtocolVersionMismatch = errors.New("bytemsg233: protocol version mismatch")

type ProtocolHello struct {
	Version       uint64
	MinCompatible uint64
}

func AppendProtocolHello(dst []byte, hello ProtocolHello) []byte {
	writer := NewWriterSize(len(dst) + 24)
	writer.buf = append(writer.buf, dst...)
	writer.WriteHeader(1, WireVarint)
	writer.WriteVarint(hello.Version)
	writer.WriteHeader(2, WireVarint)
	writer.WriteVarint(hello.MinCompatible)
	return writer.Bytes()
}

func ReadProtocolHello(data []byte) (ProtocolHello, error) {
	reader := NewReader(data)
	var hello ProtocolHello
	for !reader.EOF() {
		tag, wireType, err := reader.ReadHeader()
		if err != nil {
			return hello, err
		}
		switch tag {
		case 1:
			hello.Version, err = reader.ReadVarint()
		case 2:
			hello.MinCompatible, err = reader.ReadVarint()
		default:
			err = reader.SkipField(wireType)
		}
		if err != nil {
			return hello, err
		}
	}
	return hello, nil
}

func CheckProtocolHello(local ProtocolHello, remote ProtocolHello) error {
	if remote.Version < local.MinCompatible || local.Version < remote.MinCompatible {
		return ErrProtocolVersionMismatch
	}
	return nil
}
