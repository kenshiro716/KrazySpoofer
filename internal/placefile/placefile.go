// Package placefile finds and replaces asset ids in .rbxl and .rbxlx place files.
package placefile

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"math"
	"regexp"
	"slices"
	"strconv"
)

const (
	minIDDigits = 7
	maxIDDigits = 15
)

// Attributes are base64 blobs with length prefixes, so they are decoded and
// rebuilt. Other binary data is skipped, as base64 can look like an id.
// SourceAssetId only records where an instance came from.
var (
	xmlTokens = regexp.MustCompile(`<BinaryString name="AttributesSerialize">([^<]*)</BinaryString>` +
		`|(?s:<BinaryString[^>]*>.*?</BinaryString>)` +
		`|(?s:<SharedStrings>.*?</SharedStrings>)` +
		`|<int64 name="SourceAssetId">[^<]*</int64>` +
		`|(\d{7,15})`)
	numbers = regexp.MustCompile(`\d{7,15}`)
)

func isAlphanumeric(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_'
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

// isWholeNumber rejects digits that are part of a longer word or a decimal.
func isWholeNumber(data []byte, start, end int) bool {
	if start > 0 {
		before := data[start-1]
		if isAlphanumeric(before) || before == '.' && start > 1 && isDigit(data[start-2]) {
			return false
		}
	}
	if end < len(data) {
		after := data[end]
		if isAlphanumeric(after) || after == '.' && end+1 < len(data) && isDigit(data[end+1]) {
			return false
		}
	}
	return true
}

func parseID(token []byte) (int64, bool) {
	id, err := strconv.ParseInt(string(token), 10, 64)
	return id, err == nil && id != 0
}

// replaceNumbers calls found for every id in data and rebuilds data when an
// entry in ids matches. ids may be nil to only scan.
func replaceNumbers(data []byte, ids map[int64]int64, found func(int64)) ([]byte, int) {
	var out bytes.Buffer
	last, replaced := 0, 0
	for _, match := range numbers.FindAllIndex(data, -1) {
		start, end := match[0], match[1]
		if !isWholeNumber(data, start, end) {
			continue
		}
		id, ok := parseID(data[start:end])
		if !ok {
			continue
		}
		if found != nil {
			found(id)
		}
		if newID, exists := ids[id]; exists {
			out.Write(data[last:start])
			out.WriteString(strconv.FormatInt(newID, 10))
			last = end
			replaced++
		}
	}
	if replaced == 0 {
		return data, 0
	}
	out.Write(data[last:])
	return out.Bytes(), replaced
}

type attribute struct {
	key       []byte
	valueType byte
	value     []byte
}

const (
	attributeString = 0x02
	attributeDouble = 0x06
)

var attributeSizes = map[byte]int{
	0x03: 1,  // bool
	0x04: 4,  // int
	0x05: 4,  // float
	0x06: 8,  // double
	0x09: 8,  // UDim
	0x0A: 16, // UDim2
	0x0E: 4,  // BrickColor
	0x0F: 12, // Color3
	0x10: 8,  // Vector2
	0x11: 12, // Vector3
	0x1B: 8,  // NumberRange
	0x1C: 16, // Rect
}

// Sequences are a count followed by fixed size keypoints.
var attributeKeypointSizes = map[byte]int{
	0x17: 12, // NumberSequence
	0x19: 20, // ColorSequence
}

const (
	attributeCFrame = 0x14
	attributeEnum   = 0x15
	attributeFont   = 0x21
)

func readLength(b []byte) (int, []byte, bool) {
	if len(b) < 4 {
		return 0, nil, false
	}
	length := binary.LittleEndian.Uint32(b)
	if uint64(length) > uint64(len(b)-4) {
		return 0, nil, false
	}
	return int(length), b[4:], true
}

// attributeValueSize returns how many bytes a value takes up.
func attributeValueSize(valueType byte, b []byte) (int, bool) {
	if size, known := attributeSizes[valueType]; known {
		return size, true
	}

	switch valueType {
	case attributeString:
		length, _, ok := readLength(b)
		return 4 + length, ok
	case attributeCFrame:
		// Position, a rotation id, then a full matrix only when the id is 0.
		if len(b) < 13 {
			return 0, false
		}
		if b[12] == 0 {
			return 49, true
		}
		return 13, true
	case attributeEnum:
		length, rest, ok := readLength(b)
		if !ok || len(rest)-length < 4 {
			return 0, false
		}
		return 4 + length + 4, true
	case attributeFont:
		// Weight, style, then the family and cached face strings.
		if len(b) < 3 {
			return 0, false
		}
		family, rest, ok := readLength(b[3:])
		if !ok {
			return 0, false
		}
		face, _, ok := readLength(rest[family:])
		return 3 + 4 + family + 4 + face, ok
	}

	keypointSize, isSequence := attributeKeypointSizes[valueType]
	if !isSequence || len(b) < 4 {
		return 0, false
	}
	keypoints := binary.LittleEndian.Uint32(b)
	if uint64(keypoints)*uint64(keypointSize) > uint64(len(b)-4) {
		return 0, false
	}
	return 4 + int(keypoints)*keypointSize, true
}

// parseAttributes returns false for anything it cannot fully read, such as an
// unknown type. Callers then leave the blob exactly as it was.
func parseAttributes(b []byte) ([]attribute, bool) {
	if len(b) < 4 {
		return nil, false
	}
	count := binary.LittleEndian.Uint32(b)
	b = b[4:]

	attributes := make([]attribute, 0, min(int(count), len(b)/5+1))
	for range count {
		keyLength, rest, ok := readLength(b)
		if !ok || keyLength >= len(rest) {
			return nil, false
		}
		key := rest[:keyLength]
		valueType := rest[keyLength]
		rest = rest[keyLength+1:]

		size, ok := attributeValueSize(valueType, rest)
		if !ok || size > len(rest) {
			return nil, false
		}

		attributes = append(attributes, attribute{key, valueType, rest[:size]})
		b = rest[size:]
	}
	return attributes, len(b) == 0
}

func encodeAttributes(attributes []attribute) []byte {
	out := binary.LittleEndian.AppendUint32(nil, uint32(len(attributes)))
	for _, a := range attributes {
		out = binary.LittleEndian.AppendUint32(out, uint32(len(a.key)))
		out = append(out, a.key...)
		out = append(out, a.valueType)
		out = append(out, a.value...)
	}
	return out
}

func doubleID(value []byte) (int64, bool) {
	number := math.Float64frombits(binary.LittleEndian.Uint64(value))
	if number != math.Trunc(number) || number < 1e6 || number >= 1e15 {
		return 0, false
	}
	return int64(number), true
}

// replaceAttributeBlob returns the rebuilt blob, or the original when nothing
// changed. readable is false when the blob could not be parsed.
func replaceAttributeBlob(blob []byte, ids map[int64]int64, found func(int64)) (result []byte, replaced int, readable bool) {
	attributes, ok := parseAttributes(blob)
	if !ok {
		return blob, 0, false
	}

	for i, a := range attributes {
		switch a.valueType {
		case attributeString:
			text, count := replaceNumbers(a.value[4:], ids, found)
			if count > 0 {
				attributes[i].value = append(binary.LittleEndian.AppendUint32(nil, uint32(len(text))), text...)
				replaced += count
			}
		case attributeDouble:
			id, isID := doubleID(a.value)
			if !isID {
				continue
			}
			if found != nil {
				found(id)
			}
			if newID, exists := ids[id]; exists {
				attributes[i].value = binary.LittleEndian.AppendUint64(nil, math.Float64bits(float64(newID)))
				replaced++
			}
		}
	}
	if replaced == 0 {
		return blob, 0, true
	}
	return encodeAttributes(attributes), replaced, true
}

// replaceAttributes is replaceAttributeBlob for the base64 text in .rbxlx files.
func replaceAttributes(encoded []byte, ids map[int64]int64, found func(int64)) (result []byte, replaced int, readable bool) {
	compact := bytes.Join(bytes.Fields(encoded), nil)
	if len(compact) == 0 {
		return encoded, 0, true
	}

	blob, err := base64.StdEncoding.AppendDecode(nil, compact)
	if err != nil {
		return encoded, 0, false
	}

	rebuilt, replaced, readable := replaceAttributeBlob(blob, ids, found)
	if replaced == 0 {
		return encoded, 0, readable
	}
	return base64.StdEncoding.AppendEncode(nil, rebuilt), replaced, readable
}

// Result describes one pass over a place file.
type Result struct {
	Data              []byte
	Replaced          int
	UnreadableAttribs int
}

func process(data []byte, ids map[int64]int64, found func(int64)) (Result, error) {
	if IsBinary(data) {
		return processBinary(data, ids, found)
	}
	return processXML(data, ids, found), nil
}

func processXML(data []byte, ids map[int64]int64, found func(int64)) Result {
	var out bytes.Buffer
	result := Result{}
	last := 0

	for _, match := range xmlTokens.FindAllSubmatchIndex(data, -1) {
		var start, end int
		var replacement []byte
		var count int

		switch {
		case match[2] >= 0:
			start, end = match[2], match[3]
			rebuilt, replaced, readable := replaceAttributes(data[start:end], ids, found)
			if !readable {
				result.UnreadableAttribs++
			}
			replacement, count = rebuilt, replaced
		case match[4] >= 0:
			start, end = match[4], match[5]
			if !isWholeNumber(data, start, end) {
				continue
			}
			id, ok := parseID(data[start:end])
			if !ok {
				continue
			}
			if found != nil {
				found(id)
			}
			newID, exists := ids[id]
			if !exists {
				continue
			}
			replacement, count = strconv.AppendInt(nil, newID, 10), 1
		default:
			continue
		}

		if count == 0 {
			continue
		}
		out.Write(data[last:start])
		out.Write(replacement)
		last = end
		result.Replaced += count
	}

	if result.Replaced == 0 {
		result.Data = data
		return result
	}
	out.Write(data[last:])
	result.Data = out.Bytes()
	return result
}

// Candidates returns every number in the file that could be an asset id.
func Candidates(data []byte) (ids []int64, unreadableAttributes int, err error) {
	seen := make(map[int64]struct{})
	result, err := process(data, nil, func(id int64) {
		seen[id] = struct{}{}
	})
	if err != nil {
		return nil, 0, err
	}

	ids = make([]int64, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids, result.UnreadableAttribs, nil
}

// Replace swaps every old id in ids for its new id.
func Replace(data []byte, ids map[int64]int64) (Result, error) {
	return process(data, ids, nil)
}
