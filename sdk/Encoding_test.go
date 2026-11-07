package sdk

import "testing"

func TestUTF8Bytes(t *testing.T) {
	assertAny([]byte{97, 195, 167, 98, 226, 134, 147, 99}, UTF8Bytes("a\u00e7b\u2193c"), t)
	assertAny([]byte{240, 159, 152, 128}, UTF8Bytes("\U0001F600"), t)
	assertAny([]byte{}, UTF8Bytes(""), t)
}

func TestBase64UrlNoPadding(t *testing.T) {
	for in, want := range map[string]string{
		"": "", "t": "dA", "te": "dGU", "tes": "dGVz", "test": "dGVzdA",
		"special characters a\u00e7b\u2193c": "c3BlY2lhbCBjaGFyYWN0ZXJzIGHDp2LihpNj",
	} {
		assertAny(want, Base64UrlNoPadding(UTF8Bytes(in)), t)
	}
	assertAny("-_8", Base64UrlNoPadding([]byte{0xfb, 0xff}), t)
}

func TestHashUnitValue(t *testing.T) {
	for in, want := range map[interface{}]string{"test": "CY9rzUYh03PK3k6DJie09g", 12345.0: "gnzLDuqKcGxMNKFokfhOew", 125.0: "Pe8YStj0dV_yaYYup3OT3Q", 125.75: "BpBZt--EDwx0qBTskje27A"} {
		var got, err = HashUnitValue(in)
		assertAny(nil, err, t)
		var b = make([]byte, len(got))
		for i, c := range got {
			b[i] = byte(c)
		}
		assertAny(want, string(b), t)
	}
	var _, err = HashUnitValue(true)
	assertAny(true, err != nil, t)
}
