package sdk

// UTF8Bytes returns the bytes EncodeUTF8 writes for value. Unit UIDs are
// encoded this way before they are hashed.
func UTF8Bytes(value string) []byte {
	var buf = make([]int8, len(value)*3)
	var n = EncodeUTF8(buf, 0, value)
	var out = make([]byte, n)
	for i := 0; i < n; i++ {
		out[i] = byte(buf[i])
	}
	return out
}

// Base64UrlNoPadding encodes data with the URL-safe alphabet and no padding,
// the encoding used for hashed unit UIDs.
func Base64UrlNoPadding(data []byte) string {
	var out = make([]byte, 0, (len(data)*4+2)/3)
	var i = 0
	for ; i+3 <= len(data); i += 3 {
		var v = uint32(data[i])<<16 | uint32(data[i+1])<<8 | uint32(data[i+2])
		out = append(out, base64URLNoPaddingChars[v>>18&63], base64URLNoPaddingChars[v>>12&63],
			base64URLNoPaddingChars[v>>6&63], base64URLNoPaddingChars[v&63])
	}
	switch len(data) - i {
	case 1:
		var v = uint32(data[i]) << 16
		out = append(out, base64URLNoPaddingChars[v>>18&63], base64URLNoPaddingChars[v>>12&63])
	case 2:
		var v = uint32(data[i])<<16 | uint32(data[i+1])<<8
		out = append(out, base64URLNoPaddingChars[v>>18&63], base64URLNoPaddingChars[v>>12&63],
			base64URLNoPaddingChars[v>>6&63])
	}
	return string(out)
}
