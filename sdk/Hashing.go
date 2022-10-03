package sdk

import (
	"math/bits"
)

func HashUnit(unit string) []int8 {
	var buffer [512]int8
	var block [16]int32
	var st [4]int32

	var n = len(unit)
	var bufferLen = n << 1
	if len(buffer) < bufferLen {
		var bit = int32(32 - bits.LeadingZeros32(uint32(bufferLen-1)))
		var newbuff = make([]int8, 1<<bit)
		var encoded = EncodeUTF8(newbuff, 0, unit)
		return DigestBase64UrlNoPadding(newbuff, 0, encoded, block[:], st[:])
	} else {
		var encoded = EncodeUTF8(buffer[:], 0, unit)
		return DigestBase64UrlNoPadding(buffer[:], 0, encoded, block[:], st[:])
	}

}
