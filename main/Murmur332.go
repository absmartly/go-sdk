package main

import (
	"math/bits"
)

func Digest(key []int8, seed int) int {
	return DigestOffset(key, 0, len(key), seed)
}

func DigestOffset(key []int8, offset int, len int, seed int) int {
	var n = offset + (len & ^3)
	var hash = seed
	var i = offset
	for ; i < n; i += 4 {
		var chunk = GetUInt32(key, i)
		hash ^= scramble32(chunk)
		hash = int(int32(bits.RotateLeft32(uint32(hash), 13)))
		hash = int(int32((hash * 5) + 0xe6546b64))
	}
	switch len & 3 {
	case 3:
		hash ^= scramble32(GetUInt24(key, i))
		break
	case 2:
		hash ^= scramble32(GetUInt16(key, i))
		break
	case 1:
		hash ^= scramble32(GetUInt8(key, i))
		break
	case 0:
	default:
		break
	}
	hash ^= len
	hash = fmix32(hash)
	return hash
}

func fmix32(h int) int {
	h = int(int32(h) ^ int32(uint32(h)>>16))
	h = int(int32(h * 0x85ebca6b))
	h = int(int32(h) ^ int32(uint32(h)>>13))
	h = int(int32(h * 0xc2b2ae35))
	h = int(int32(h) ^ int32(uint32(h)>>16))

	return h
}

func scramble32(block uint32) int {
	return int(int32(bits.RotateLeft32(uint32(int32(block*0xcc9e2d51)), 15)) * 0x1b873593)
}
