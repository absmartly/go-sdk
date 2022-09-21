package sdk

func PutUInt32(buf []int8, offset int, x int) {
	buf[offset] = (int8)(x & 0xff)
	buf[offset+1] = (int8)((x >> 8) & 0xff)
	buf[offset+2] = (int8)((x >> 16) & 0xff)
	buf[offset+3] = (int8)((x >> 24) & 0xff)
}

func GetUInt32(buf []int8, offset int) uint32 {
	return uint32(int32(byte(buf[offset])&0xff) |
		(int32(byte(buf[offset+1])&0xff) << 8) |
		(int32(byte(buf[offset+2])&0xff) << 16) |
		(int32(byte(buf[offset+3])&0xff) << 24))
}

func GetUInt24(buf []int8, offset int) uint32 {
	return uint32(int(byte(buf[offset])&0xff) |
		(int(byte(buf[offset+1])&0xff) << 8) |
		(int(byte(buf[offset+2])&0xff) << 16))
}

func GetUInt16(buf []int8, offset int) uint32 {
	return uint32(int(byte(buf[offset])&0xff) |
		(int(byte(buf[offset+1])&0xff) << 8))
}

func GetUInt8(buf []int8, offset int) uint32 {
	return uint32(int(byte(buf[offset]) & 0xff))
}

func EncodeUTF8(buf []byte, offset int, value string) int {
	var n = len(value)

	var out = offset
	for i := 0; i < n; i++ {
		var c = value[i]
		if c < 0x80 {
			buf[out] = c
			out++
		} else if c == 0x80 {
			buf[out] = (c >> 6) | 192
			out++
			buf[out] = (c & 63) | 128
			out++
		}
	}
	return out - offset
}
