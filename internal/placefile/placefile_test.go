package placefile

import (
	"encoding/base64"
	"encoding/binary"
	"math"
	"slices"
	"strings"
	"testing"
)

func attributeBlob(entries ...attribute) string {
	return base64.StdEncoding.EncodeToString(encodeAttributes(entries))
}

func stringAttribute(key, value string) attribute {
	raw := binary.LittleEndian.AppendUint32(nil, uint32(len(value)))
	return attribute{[]byte(key), attributeString, append(raw, value...)}
}

func doubleAttribute(key string, value float64) attribute {
	return attribute{[]byte(key), attributeDouble, binary.LittleEndian.AppendUint64(nil, math.Float64bits(value))}
}

func TestCandidatesAndReplace(t *testing.T) {
	file := `<roblox>
<Content name="SoundId"><url>rbxassetid://1234567</url></Content>
<ProtectedString name="Source"><![CDATA[local a = 7654321 local b = 12345678 local c = 1234567.5]]></ProtectedString>
<int64 name="SourceAssetId">2222222</int64>
<BinaryString name="PhysicsData">MTIzNDU2Nw==3333333</BinaryString>
<string name="Word">abc1234567</string>
<SharedStrings><SharedString md5="x">4444444</SharedString></SharedStrings>
</roblox>`

	ids, unreadable, _ := Candidates([]byte(file))
	if unreadable != 0 || !slices.Equal(ids, []int64{1234567, 7654321, 12345678}) {
		t.Fatalf("ids %v unreadable %d", ids, unreadable)
	}

	result, _ := Replace([]byte(file), map[int64]int64{1234567: 99999999999, 7654321: 8888888})
	output := string(result.Data)
	if result.Replaced != 2 {
		t.Fatalf("replaced %d", result.Replaced)
	}
	for _, want := range []string{"rbxassetid://99999999999", "local a = 8888888", "local b = 12345678", "local c = 1234567.5", "abc1234567", ">2222222<"} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing %q in %s", want, output)
		}
	}
}

func TestAttributes(t *testing.T) {
	vector := attribute{[]byte("Vector"), 0x11, make([]byte, 12)}
	sequence := attribute{[]byte("Sequence"), 0x17, append(binary.LittleEndian.AppendUint32(nil, 2), make([]byte, 24)...)}
	blob := attributeBlob(
		stringAttribute("Hit", "rbxassetid://1234567"),
		doubleAttribute("Id", 7654321),
		doubleAttribute("Speed", 16.5),
		vector,
		sequence,
	)
	file := `<BinaryString name="AttributesSerialize">` + blob + `</BinaryString>`

	ids, unreadable, _ := Candidates([]byte(file))
	if unreadable != 0 || !slices.Equal(ids, []int64{1234567, 7654321}) {
		t.Fatalf("ids %v unreadable %d", ids, unreadable)
	}

	result, _ := Replace([]byte(file), map[int64]int64{1234567: 99999999999, 7654321: 8888888})
	if result.Replaced != 2 {
		t.Fatalf("replaced %d", result.Replaced)
	}

	encoded := strings.TrimSuffix(strings.TrimPrefix(string(result.Data), `<BinaryString name="AttributesSerialize">`), `</BinaryString>`)
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	attributes, ok := parseAttributes(decoded)
	if !ok || len(attributes) != 5 {
		t.Fatalf("parsed %v %v", ok, attributes)
	}
	if text := string(attributes[0].value[4:]); text != "rbxassetid://99999999999" {
		t.Fatalf("string %q", text)
	}
	if id, _ := doubleID(attributes[1].value); id != 8888888 {
		t.Fatalf("double %d", id)
	}
}

func TestUnknownAttributeIsLeftAlone(t *testing.T) {
	blob := attributeBlob(stringAttribute("Hit", "1234567"), attribute{[]byte("Odd"), 0x7F, []byte{1, 2, 3}})
	file := `<BinaryString name="AttributesSerialize">` + blob + `</BinaryString>`

	if _, unreadable, _ := Candidates([]byte(file)); unreadable != 1 {
		t.Fatalf("unreadable %d", unreadable)
	}
	if result, _ := Replace([]byte(file), map[int64]int64{1234567: 7654321}); string(result.Data) != file {
		t.Fatal("blob changed")
	}
}

func TestTruncatedAttributesAreLeftAlone(t *testing.T) {
	blob := base64.StdEncoding.EncodeToString([]byte{5, 0, 0, 0, 200, 0, 0})
	if _, ok := parseAttributes([]byte{5, 0, 0, 0, 200, 0, 0}); ok {
		t.Fatal("parsed truncated blob")
	}
	file := `<BinaryString name="AttributesSerialize">` + blob + `</BinaryString>`
	if result, _ := Replace([]byte(file), map[int64]int64{1: 2}); string(result.Data) != file {
		t.Fatal("blob changed")
	}
}
