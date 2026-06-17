package bytemsg233

import "testing"

func TestReaderSkipsUnknownFields(t *testing.T) {
	writer := NewWriter()
	writer.WriteHeader(99, WireVarint)
	writer.WriteVarint(9001)
	writer.WriteHeader(100, WireBytes)
	writer.WriteStringValue("future")
	writer.WriteHeader(101, WireFixed32)
	writer.WriteFixed32(0x12345678)
	writer.WriteHeader(102, WireFixed64)
	writer.WriteFixed64(0x0102030405060708)
	writer.WriteHeader(1, WireVarint)
	writer.WriteVarint(42)
	writer.WriteHeader(2, WireBytes)
	writer.WriteStringValue("stable")

	reader := NewReader(writer.Bytes())
	var id uint64
	var name string
	for !reader.EOF() {
		tag, wireType, err := reader.ReadHeader()
		if err != nil {
			t.Fatalf("ReadHeader failed: %v", err)
		}
		switch tag {
		case 1:
			id, err = reader.ReadVarint()
		case 2:
			name, err = reader.ReadStringView()
		default:
			err = reader.SkipField(wireType)
		}
		if err != nil {
			t.Fatalf("read tag %d failed: %v", tag, err)
		}
	}
	if id != 42 || name != "stable" {
		t.Fatalf("decoded old fields = (%d, %q), want (42, stable)", id, name)
	}
}

func TestProtocolHello(t *testing.T) {
	local := ProtocolHello{Version: 7, Fingerprint: 0xabc, MinCompatible: 6}
	data := AppendProtocolHello(nil, local)
	remote, err := ReadProtocolHello(data)
	if err != nil {
		t.Fatalf("ReadProtocolHello failed: %v", err)
	}
	if remote != local {
		t.Fatalf("hello = %#v, want %#v", remote, local)
	}
	if err := CheckProtocolHello(local, remote); err != nil {
		t.Fatalf("CheckProtocolHello failed: %v", err)
	}
}
