package placefile

import (
	"bytes"
	"encoding/binary"
	"slices"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func binaryString(value string) []byte {
	return append(binary.LittleEndian.AppendUint32(nil, uint32(len(value))), value...)
}

func rawChunk(name string, payload []byte) []byte {
	header := make([]byte, chunkHeaderSize)
	copy(header, name)
	binary.LittleEndian.PutUint32(header[8:], uint32(len(payload)))
	return append(header, payload...)
}

func zstdChunk(t *testing.T, name string, payload []byte) []byte {
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer encoder.Close()

	compressed := encoder.EncodeAll(payload, nil)
	header := make([]byte, chunkHeaderSize)
	copy(header, name)
	binary.LittleEndian.PutUint32(header[4:], uint32(len(compressed)))
	binary.LittleEndian.PutUint32(header[8:], uint32(len(payload)))
	return append(header, compressed...)
}

func instChunk(classID uint32, className string, count uint32) []byte {
	payload := binary.LittleEndian.AppendUint32(nil, classID)
	payload = append(payload, binaryString(className)...)
	payload = append(payload, 0)
	return binary.LittleEndian.AppendUint32(payload, count)
}

func propChunk(classID uint32, name string, valueType byte, values []byte) []byte {
	payload := binary.LittleEndian.AppendUint32(nil, classID)
	payload = append(payload, binaryString(name)...)
	payload = append(payload, valueType)
	return append(payload, values...)
}

func TestBinaryPlace(t *testing.T) {
	intValues := make([]byte, 16)
	encodeInt64(intValues, 0, 2, 7654321)
	encodeInt64(intValues, 1, 2, 5)

	place := make([]byte, binaryHeaderSize)
	copy(place, binaryMagic)
	place = append(place, zstdChunk(t, "INST", instChunk(0, "Sound", 1))...)
	place = append(place, rawChunk("INST", instChunk(1, "IntValue", 2))...)
	place = append(place, rawChunk("INST", instChunk(2, "Part", 1))...)
	place = append(place, zstdChunk(t, "PROP", propChunk(0, "SoundId", typeString, binaryString("rbxassetid://1234567")))...)
	place = append(place, rawChunk("PROP", propChunk(1, "Value", typeInt64, intValues))...)
	place = append(place, rawChunk("PROP", propChunk(2, "PhysicsData", typeString, binaryString("1234567")))...)
	place = append(place, rawChunk("END\x00", []byte("</roblox>"))...)

	ids, unreadable, err := Candidates(place)
	if err != nil || unreadable != 0 || !slices.Equal(ids, []int64{1234567, 7654321}) {
		t.Fatalf("ids %v unreadable %d err %v", ids, unreadable, err)
	}

	result, err := Replace(place, map[int64]int64{1234567: 99999999999, 7654321: 8888888})
	if err != nil || result.Replaced != 2 {
		t.Fatalf("replaced %d err %v", result.Replaced, err)
	}

	after, _, err := Candidates(result.Data)
	if err != nil || !slices.Equal(after, []int64{8888888, 99999999999}) {
		t.Fatalf("after %v err %v", after, err)
	}
	if !bytes.Contains(result.Data, []byte("PhysicsData\x01\x07\x00\x00\x001234567")) {
		t.Fatal("binary data property changed")
	}
}

func TestCorruptBinaryPlace(t *testing.T) {
	place := append(make([]byte, 0, 64), binaryMagic...)
	place = append(place, make([]byte, binaryHeaderSize-len(binaryMagic))...)
	place = append(place, 'P', 'R', 'O', 'P', 255, 255, 255, 255)
	if _, _, err := Candidates(place); err == nil {
		t.Fatal("expected an error")
	}
}

func TestDecompressLZ4(t *testing.T) {
	// "abcabcabcabc": 3 literals, then a 9 byte match at offset 3.
	out, err := decompressLZ4([]byte{0x35, 'a', 'b', 'c', 3, 0}, 12)
	if err != nil || string(out) != "abcabcabcabc" {
		t.Fatalf("%q %v", out, err)
	}
}

func TestVariableAttributeTypes(t *testing.T) {
	font := append([]byte{0xBC, 0x02, 0}, binaryString("rbxasset://fonts/a.json")...)
	font = append(font, binaryString("")...)
	enum := binary.LittleEndian.AppendUint32(binaryString("LightingStyle"), 1)

	entries := []attribute{
		{[]byte("Rotation"), attributeCFrame, append(make([]byte, 12), 2)},
		{[]byte("Matrix"), attributeCFrame, make([]byte, 49)},
		{[]byte("Style"), attributeEnum, enum},
		{[]byte("Label"), attributeFont, font},
		{[]byte("Count"), 0x04, []byte{3, 0, 0, 0}},
	}
	parsed, ok := parseAttributes(encodeAttributes(entries))
	if !ok || len(parsed) != len(entries) {
		t.Fatalf("parsed %v %v", ok, parsed)
	}
}
