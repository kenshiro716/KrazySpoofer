package placefile

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"github.com/klauspost/compress/zstd"
)

const (
	binaryHeaderSize = 32
	chunkHeaderSize  = 16

	typeString  = 0x01
	typeFloat64 = 0x05
	typeInt64   = 0x1B
)

var (
	binaryMagic = []byte("<roblox!\x89\xff\r\n\x1a\n")
	zstdMagic   = []byte{0x28, 0xB5, 0x2F, 0xFD}

	errCorrupt = errors.New("place file is damaged or in an unknown format")
)

// IsBinary reports whether data is a binary .rbxl file.
func IsBinary(data []byte) bool {
	return bytes.HasPrefix(data, binaryMagic)
}

type chunk struct {
	name             string
	compressedSize   uint32
	uncompressedSize uint32
	start, end       int // Whole chunk, including its header.
}

func (c chunk) stored(data []byte) []byte {
	return data[c.start+chunkHeaderSize : c.end]
}

func readChunks(data []byte) ([]chunk, error) {
	if len(data) < binaryHeaderSize {
		return nil, errCorrupt
	}

	chunks := make([]chunk, 0, 1024)
	for offset := binaryHeaderSize; offset < len(data); {
		if len(data)-offset < chunkHeaderSize {
			return nil, errCorrupt
		}

		c := chunk{
			name:             string(bytes.TrimRight(data[offset:offset+4], "\x00")),
			compressedSize:   binary.LittleEndian.Uint32(data[offset+4:]),
			uncompressedSize: binary.LittleEndian.Uint32(data[offset+8:]),
			start:            offset,
		}

		size := c.compressedSize
		if size == 0 {
			size = c.uncompressedSize
		}
		if uint64(size) > uint64(len(data)-offset-chunkHeaderSize) {
			return nil, errCorrupt
		}

		c.end = offset + chunkHeaderSize + int(size)
		chunks = append(chunks, c)
		offset = c.end

		if c.name == "END" {
			break
		}
	}
	return chunks, nil
}

// decompressLZ4 decodes one LZ4 block into a buffer of a known size.
func decompressLZ4(src []byte, size int) ([]byte, error) {
	dst := make([]byte, 0, size)
	for i := 0; i < len(src); {
		token := src[i]
		i++

		literals := int(token >> 4)
		if literals == 15 {
			for {
				if i >= len(src) {
					return nil, errCorrupt
				}
				extra := src[i]
				i++
				literals += int(extra)
				if extra != 255 {
					break
				}
			}
		}
		if literals > len(src)-i {
			return nil, errCorrupt
		}
		dst = append(dst, src[i:i+literals]...)
		i += literals
		if i == len(src) {
			break
		}

		if i+2 > len(src) {
			return nil, errCorrupt
		}
		offset := int(src[i]) | int(src[i+1])<<8
		i += 2

		length := int(token&0x0F) + 4
		if token&0x0F == 15 {
			for {
				if i >= len(src) {
					return nil, errCorrupt
				}
				extra := src[i]
				i++
				length += int(extra)
				if extra != 255 {
					break
				}
			}
		}

		match := len(dst) - offset
		if offset == 0 || match < 0 {
			return nil, errCorrupt
		}
		for j := 0; j < length; j++ {
			dst = append(dst, dst[match+j])
		}
	}

	if len(dst) != size {
		return nil, errCorrupt
	}
	return dst, nil
}

func (c chunk) payload(data []byte, decoder *zstd.Decoder) ([]byte, error) {
	stored := c.stored(data)
	switch {
	case c.compressedSize == 0:
		return stored, nil
	case bytes.HasPrefix(stored, zstdMagic):
		return decoder.DecodeAll(stored, make([]byte, 0, c.uncompressedSize))
	default:
		return decompressLZ4(stored, int(c.uncompressedSize))
	}
}

type binaryClass struct {
	name  string
	count int
}

func readBinaryString(b []byte) (string, []byte, bool) {
	length, rest, ok := readLength(b)
	if !ok {
		return "", nil, false
	}
	return string(rest[:length]), rest[length:], true
}

func readClass(payload []byte) (int32, binaryClass, bool) {
	if len(payload) < 4 {
		return 0, binaryClass{}, false
	}
	id := int32(binary.LittleEndian.Uint32(payload))
	name, rest, ok := readBinaryString(payload[4:])
	if !ok || len(rest) < 5 {
		return 0, binaryClass{}, false
	}
	return id, binaryClass{name, int(binary.LittleEndian.Uint32(rest[1:]))}, true
}

// Only these hold ids. Other string properties can be raw binary data, such as
// PhysicsData, where digits would be meaningless.
var binaryTextProperties = map[string]bool{
	"Source":       true,
	"LinkedSource": true,
	"SoundId":      true,
	"AnimationId":  true,
	"MeshId":       true,
}

const attributesProperty = "AttributesSerialize"

func rewriteStrings(values []byte, count int, rewrite func([]byte) []byte) ([]byte, bool) {
	var out bytes.Buffer
	out.Grow(len(values))
	for range count {
		length, rest, ok := readLength(values)
		if !ok {
			return nil, false
		}
		value := rewrite(rest[:length])
		binary.Write(&out, binary.LittleEndian, uint32(len(value)))
		out.Write(value)
		values = rest[length:]
	}
	return out.Bytes(), len(values) == 0
}

func decodeInt64(values []byte, index, count int) int64 {
	var encoded uint64
	for b := 0; b < 8; b++ {
		encoded = encoded<<8 | uint64(values[b*count+index])
	}
	return int64(encoded>>1) ^ -int64(encoded&1)
}

func encodeInt64(values []byte, index, count int, value int64) {
	encoded := uint64(value<<1) ^ uint64(value>>63)
	for b := 0; b < 8; b++ {
		values[b*count+index] = byte(encoded >> (56 - 8*b))
	}
}

func isIDNumber(id int64) bool {
	return id >= 1_000_000 && id < 1_000_000_000_000_000
}

type binaryPass struct {
	ids        map[int64]int64
	found      func(int64)
	replaced   int
	unreadable int
}

// rewriteProperty returns the new values, or nil when nothing changed.
func (p *binaryPass) rewriteProperty(class binaryClass, name string, valueType byte, values []byte) ([]byte, error) {
	count := class.count

	switch {
	case valueType == typeString && name == attributesProperty:
		changed := 0
		out, ok := rewriteStrings(values, count, func(blob []byte) []byte {
			if len(blob) == 0 {
				return blob
			}
			rebuilt, replaced, readable := replaceAttributeBlob(blob, p.ids, p.found)
			if !readable {
				p.unreadable++
			}
			changed += replaced
			return rebuilt
		})
		if !ok {
			return nil, errCorrupt
		}
		p.replaced += changed
		if changed == 0 {
			return nil, nil
		}
		return out, nil

	case valueType == typeString && (binaryTextProperties[name] || name == "Value" && class.name == "StringValue"):
		changed := 0
		out, ok := rewriteStrings(values, count, func(text []byte) []byte {
			rebuilt, replaced := replaceNumbers(text, p.ids, p.found)
			changed += replaced
			return rebuilt
		})
		if !ok {
			return nil, errCorrupt
		}
		p.replaced += changed
		if changed == 0 {
			return nil, nil
		}
		return out, nil

	case valueType == typeFloat64 && name == "Value" && class.name == "NumberValue":
		if len(values) < 8*count {
			return nil, errCorrupt
		}
		var out []byte
		for i := range count {
			value := values[i*8 : i*8+8]
			id, isID := doubleID(value)
			if !isID {
				continue
			}
			if p.found != nil {
				p.found(id)
			}
			newID, exists := p.ids[id]
			if !exists {
				continue
			}
			if out == nil {
				out = bytes.Clone(values)
			}
			binary.LittleEndian.PutUint64(out[i*8:], math.Float64bits(float64(newID)))
			p.replaced++
		}
		return out, nil

	case valueType == typeInt64 && (name == "Value" && class.name == "IntValue" || name == "MeshId" && class.name == "CharacterMesh"):
		if len(values) < 8*count {
			return nil, errCorrupt
		}
		var out []byte
		for i := range count {
			id := decodeInt64(values, i, count)
			if !isIDNumber(id) {
				continue
			}
			if p.found != nil {
				p.found(id)
			}
			newID, exists := p.ids[id]
			if !exists {
				continue
			}
			if out == nil {
				out = bytes.Clone(values)
			}
			encodeInt64(out, i, count, newID)
			p.replaced++
		}
		return out, nil
	}
	return nil, nil
}

func processBinary(data []byte, ids map[int64]int64, found func(int64)) (Result, error) {
	chunks, err := readChunks(data)
	if err != nil {
		return Result{}, err
	}

	decoder, err := zstd.NewReader(nil, zstd.WithDecoderConcurrency(1))
	if err != nil {
		return Result{}, err
	}
	defer decoder.Close()

	classes := make(map[int32]binaryClass)
	for _, c := range chunks {
		if c.name != "INST" {
			continue
		}
		payload, err := c.payload(data, decoder)
		if err != nil {
			return Result{}, fmt.Errorf("%w: %v", errCorrupt, err)
		}
		id, class, ok := readClass(payload)
		if !ok {
			return Result{}, errCorrupt
		}
		classes[id] = class
	}

	pass := &binaryPass{ids: ids, found: found}
	replacements := make(map[int][]byte)

	for i, c := range chunks {
		if c.name != "PROP" {
			continue
		}
		payload, err := c.payload(data, decoder)
		if err != nil {
			return Result{}, fmt.Errorf("%w: %v", errCorrupt, err)
		}
		if len(payload) < 4 {
			return Result{}, errCorrupt
		}

		classID := int32(binary.LittleEndian.Uint32(payload))
		name, rest, ok := readBinaryString(payload[4:])
		if !ok || len(rest) == 0 {
			return Result{}, errCorrupt
		}
		class, known := classes[classID]
		if !known {
			return Result{}, errCorrupt
		}

		values := rest[1:]
		rebuilt, err := pass.rewriteProperty(class, name, rest[0], values)
		if err != nil {
			return Result{}, err
		}
		if rebuilt != nil {
			head := len(payload) - len(values)
			replacements[i] = append(bytes.Clone(payload[:head]), rebuilt...)
		}
	}

	result := Result{Data: data, Replaced: pass.replaced, UnreadableAttribs: pass.unreadable}
	if len(replacements) == 0 {
		return result, nil
	}

	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1))
	if err != nil {
		return Result{}, err
	}
	defer encoder.Close()

	// Rewritten chunks are compressed the way Studio does. All others keep
	// their original bytes.
	var out bytes.Buffer
	out.Grow(len(data))
	out.Write(data[:binaryHeaderSize])
	for i, c := range chunks {
		payload, changed := replacements[i]
		if !changed {
			out.Write(data[c.start:c.end])
			continue
		}

		header := make([]byte, chunkHeaderSize)
		copy(header, data[c.start:c.start+4])
		compressed := encoder.EncodeAll(payload, nil)
		binary.LittleEndian.PutUint32(header[4:], uint32(len(compressed)))
		binary.LittleEndian.PutUint32(header[8:], uint32(len(payload)))
		out.Write(header)
		out.Write(compressed)
	}
	result.Data = out.Bytes()
	return result, nil
}
