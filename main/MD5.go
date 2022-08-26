package main

import (
	"math/bits"
)

func DigestBase64UrlNoPadding(key []byte, offset int, length int, block []int32, st []int32) []int8 {
	var dst = make([]int8, len(key))
	for i := 0; i < len(key); i++ {
		dst[i] = int8(key[i])
	}
	var state = md5state(dst, offset, length, block, st)
	var a = state[0]
	var b = state[1]
	var c = state[2]
	var d = state[3]

	var result [22]int8

	var t = a
	result[0] = int8(base64URLNoPaddingChars[uint32(t>>2)&63])
	result[1] = int8(base64URLNoPaddingChars[(uint32(t&3)<<4)|((uint32(t>>12))&15)])
	result[2] = int8(base64URLNoPaddingChars[(((uint32(t>>8) & 15) << 2) | (uint32(t>>22) & 3))])
	result[3] = int8(base64URLNoPaddingChars[uint32(t>>16)&63])

	t = int32(uint32(a)>>24 | (uint32(b << 8)))
	result[4] = int8(base64URLNoPaddingChars[uint32(t>>2)&63])
	result[5] = int8(base64URLNoPaddingChars[(uint32(t&3)<<4)|((uint32(t>>12))&15)])
	result[6] = int8(base64URLNoPaddingChars[(((uint32(t>>8) & 15) << 2) | (uint32(t>>22) & 3))])
	result[7] = int8(base64URLNoPaddingChars[uint32(t>>16)&63])

	t = int32(uint32(b)>>16 | uint32(c<<16))
	result[8] = int8(base64URLNoPaddingChars[uint32(t>>2)&63])
	result[9] = int8(base64URLNoPaddingChars[(uint32(t&3)<<4)|((uint32(t>>12))&15)])
	result[10] = int8(base64URLNoPaddingChars[(((uint32(t>>8) & 15) << 2) | (uint32(t>>22) & 3))])
	result[11] = int8(base64URLNoPaddingChars[uint32(t>>16)&63])
	t = int32(uint32(c >> 8))
	result[12] = int8(base64URLNoPaddingChars[uint32(t>>2)&63])
	result[13] = int8(base64URLNoPaddingChars[(uint32(t&3)<<4)|((uint32(t>>12))&15)])
	result[14] = int8(base64URLNoPaddingChars[(((uint32(t>>8) & 15) << 2) | (uint32(t>>22) & 3))])
	result[15] = int8(base64URLNoPaddingChars[uint32(t>>16)&63])

	t = d
	result[16] = int8(base64URLNoPaddingChars[uint32(t>>2)&63])
	result[17] = int8(base64URLNoPaddingChars[(uint32(t&3)<<4)|((uint32(t>>12))&15)])
	result[18] = int8(base64URLNoPaddingChars[(((uint32(t>>8) & 15) << 2) | (uint32(t>>22) & 3))])
	result[19] = int8(base64URLNoPaddingChars[uint32(t>>16)&63])

	t = int32(uint32(d >> 24))
	result[20] = int8(base64URLNoPaddingChars[uint32(t>>2)&63])
	result[21] = int8(base64URLNoPaddingChars[(uint32(t&3) << 4)])

	return result[:]
}

func cmn(q int32, a int32, b int32, x int32, s int32, t int32) int32 {
	a = a + q + x + t
	return int32(bits.RotateLeft32(uint32(a), int(s))) + b
}

func ff(a int32, b int32, c int32, d int32, x int32, s int32, t int32) int32 {
	return cmn((b&c)|(^b&d), a, b, x, s, t)
}

func gg(a int32, b int32, c int32, d int32, x int32, s int32, t int32) int32 {
	return cmn((b&d)|(c & ^d), a, b, x, s, t)
}

func hh(a int32, b int32, c int32, d int32, x int32, s int32, t int32) int32 {
	return cmn(b^c^d, a, b, x, s, t)
}

func ii(a int32, b int32, c int32, d int32, x int32, s int32, t int32) int32 {
	return cmn(c^(b|^d), a, b, x, s, t)
}

func md5cycle(x []int32, k []int32) {
	var a = x[0]
	var b = x[1]
	var c = x[2]
	var d = x[3]

	a = ff(a, b, c, d, k[0], 7, -680876936)
	d = ff(d, a, b, c, k[1], 12, -389564586)
	c = ff(c, d, a, b, k[2], 17, 606105819)
	b = ff(b, c, d, a, k[3], 22, -1044525330)
	a = ff(a, b, c, d, k[4], 7, -176418897)
	d = ff(d, a, b, c, k[5], 12, 1200080426)
	c = ff(c, d, a, b, k[6], 17, -1473231341)
	b = ff(b, c, d, a, k[7], 22, -45705983)
	a = ff(a, b, c, d, k[8], 7, 1770035416)
	d = ff(d, a, b, c, k[9], 12, -1958414417)
	c = ff(c, d, a, b, k[10], 17, -42063)
	b = ff(b, c, d, a, k[11], 22, -1990404162)
	a = ff(a, b, c, d, k[12], 7, 1804603682)
	d = ff(d, a, b, c, k[13], 12, -40341101)
	c = ff(c, d, a, b, k[14], 17, -1502002290)
	b = ff(b, c, d, a, k[15], 22, 1236535329)

	a = gg(a, b, c, d, k[1], 5, -165796510)
	d = gg(d, a, b, c, k[6], 9, -1069501632)
	c = gg(c, d, a, b, k[11], 14, 643717713)
	b = gg(b, c, d, a, k[0], 20, -373897302)
	a = gg(a, b, c, d, k[5], 5, -701558691)
	d = gg(d, a, b, c, k[10], 9, 38016083)
	c = gg(c, d, a, b, k[15], 14, -660478335)
	b = gg(b, c, d, a, k[4], 20, -405537848)
	a = gg(a, b, c, d, k[9], 5, 568446438)
	d = gg(d, a, b, c, k[14], 9, -1019803690)
	c = gg(c, d, a, b, k[3], 14, -187363961)
	b = gg(b, c, d, a, k[8], 20, 1163531501)
	a = gg(a, b, c, d, k[13], 5, -1444681467)
	d = gg(d, a, b, c, k[2], 9, -51403784)
	c = gg(c, d, a, b, k[7], 14, 1735328473)
	b = gg(b, c, d, a, k[12], 20, -1926607734)

	a = hh(a, b, c, d, k[5], 4, -378558)
	d = hh(d, a, b, c, k[8], 11, -2022574463)
	c = hh(c, d, a, b, k[11], 16, 1839030562)
	b = hh(b, c, d, a, k[14], 23, -35309556)
	a = hh(a, b, c, d, k[1], 4, -1530992060)
	d = hh(d, a, b, c, k[4], 11, 1272893353)
	c = hh(c, d, a, b, k[7], 16, -155497632)
	b = hh(b, c, d, a, k[10], 23, -1094730640)
	a = hh(a, b, c, d, k[13], 4, 681279174)
	d = hh(d, a, b, c, k[0], 11, -358537222)
	c = hh(c, d, a, b, k[3], 16, -722521979)
	b = hh(b, c, d, a, k[6], 23, 76029189)
	a = hh(a, b, c, d, k[9], 4, -640364487)
	d = hh(d, a, b, c, k[12], 11, -421815835)
	c = hh(c, d, a, b, k[15], 16, 530742520)
	b = hh(b, c, d, a, k[2], 23, -995338651)

	a = ii(a, b, c, d, k[0], 6, -198630844)
	d = ii(d, a, b, c, k[7], 10, 1126891415)
	c = ii(c, d, a, b, k[14], 15, -1416354905)
	b = ii(b, c, d, a, k[5], 21, -57434055)
	a = ii(a, b, c, d, k[12], 6, 1700485571)
	d = ii(d, a, b, c, k[3], 10, -1894986606)
	c = ii(c, d, a, b, k[10], 15, -1051523)
	b = ii(b, c, d, a, k[1], 21, -2054922799)
	a = ii(a, b, c, d, k[8], 6, 1873313359)
	d = ii(d, a, b, c, k[15], 10, -30611744)
	c = ii(c, d, a, b, k[6], 15, -1560198380)
	b = ii(b, c, d, a, k[13], 21, 1309151649)
	a = ii(a, b, c, d, k[4], 6, -145523070)
	d = ii(d, a, b, c, k[11], 10, -1120210379)
	c = ii(c, d, a, b, k[2], 15, 718787259)
	b = ii(b, c, d, a, k[9], 21, -343485551)

	x[0] += a
	x[1] += b
	x[2] += c
	x[3] += d
}

func md5state(key []int8, offset int, len int, block []int32, state []int32) []int32 {
	var n = offset + (len & ^63)

	state[0] = 1732584193
	state[1] = -271733879
	state[2] = -1732584194
	state[3] = 271733878

	var i = offset
	for ; i < n; i += 64 {
		for w := 0; w < 16; w++ {
			block[w] = int32(GetUInt32(key, i+(w<<2)))
		}

		md5cycle(state, block)
	}

	var m = len & ^3
	var w = 0
	for ; i < m; i += 4 {
		block[w] = int32(GetUInt32(key, i))
		w++
	}

	switch len & 3 {
	case 3:
		block[w] = int32(GetUInt24(key, i) | 0x80000000)
		w++
		break
	case 2:
		block[w] = int32(GetUInt16(key, i) | 0x800000)
		w++
		break
	case 1:
		block[w] = int32(GetUInt8(key, i) | 0x8000)
		w++
		break
	default:
		block[w] = 0x80
		w++
		break
	}

	if w > 14 {
		if w < 16 {
			block[w] = 0
		}

		md5cycle(state, block)
		w = 0
	}

	for ; w < 16; w++ {
		block[w] = 0
	}

	block[14] = int32(len << 3)
	md5cycle(state, block)
	return state
}

var base64URLNoPaddingChars = []byte{'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'I', 'J', 'K', 'L',
	'M', 'N', 'O', 'P', 'Q', 'R', 'S', 'T', 'U', 'V', 'W', 'X', 'Y', 'Z', 'a', 'b', 'c', 'd', 'e', 'f', 'g',
	'h', 'i', 'j', 'k', 'l', 'm', 'n', 'o', 'p', 'q', 'r', 's', 't', 'u', 'v', 'w', 'x', 'y', 'z', '0', '1',
	'2', '3', '4', '5', '6', '7', '8', '9', '-', '_'}
