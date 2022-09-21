package sdk

import (
	"math/bits"
)

func HashUnit(unit string, buffer []byte, block []int32, st []int32) []int8 {
	var n = len(unit)
	var bufferLen = n << 1

	if len(buffer) < bufferLen {
		var bit = int32(32 - bits.LeadingZeros32(uint32(bufferLen-1)))
		buffer = make([]byte, 1<<bit)
	}

	var encoded = EncodeUTF8(buffer, 0, unit)
	return DigestBase64UrlNoPadding(buffer, 0, encoded, block, st)
}
