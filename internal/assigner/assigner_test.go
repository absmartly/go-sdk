package assigner

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Next tests check all hash and encoding parameters are identical between different SDKs.

// TestHashUnit checks initial unit hashing. Test cases are taken from:
// https://github.com/absmartly/javascript-sdk/blob/311acea8966a0db92b8b7996c4d4752125fe968b/src/__tests__/md5.test.js
func TestHashUnit(t *testing.T) {
	cases := []struct{ unit, hash string }{
		{"", "1B2M2Y8AsgTpgAmY7PhCfg"},
		{" ", "chXunH2dwinSkhpA6JnsXw"},
		{"t", "41jvpIn1gGLxDdcxa2Vkng"},
		{"te", "Vp73JkK-D63XEdakaNaO4Q"},
		{"tes", "KLZi2IO212_Zbk3cXpungA"},
		{"test", "CY9rzUYh03PK3k6DJie09g"},
		{"testy", "K5I_V6RgP8c6sYKz-TVn8g"},
		{"testy1", "8fT8xGipOhPkZ2DncKU-1A"},
		{"testy12", "YqRAtOz000gIu61ErEH18A"},
		{"testy123", "pfV2H07L6WvdqlY0zHuYIw"},
		{"special characters açb↓c", "4PIrO7lKtTxOcj2eMYlG7A"},
		{"The quick brown fox jumps over the lazy dog", "nhB9nTcrtoJr2B01QqQZ1g"},
		{"The quick brown fox jumps over the lazy dog and eats a pie", "iM-8ECRrLUQzixl436y96A"},
		{
			"Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat. Duis aute irure dolor in reprehenderit in voluptate velit esse cillum dolore eu fugiat nulla pariatur. Excepteur sint occaecat cupidatat non proident, sunt in culpa qui officia deserunt mollit anim id est laborum.",
			"24m7XOq4f5wPzCqzbBicLA",
		},
	}
	a := &Assigner{}
	for _, c := range cases {
		assert.Equal(t, c.hash, a.hashUnit(c.unit), "hash(%.32s)", c.unit)
	}
}

// TestHashUnit checks Murmur3 hashing. Test cases are taken from:
// https://github.com/absmartly/javascript-sdk/blob/311acea8966a0db92b8b7996c4d4752125fe968b/src/__tests__/murmur3_32.test.js
func TestHashMur(t *testing.T) {
	cases := []struct {
		data       string
		seed, hash uint32
	}{
		{"", 0x00000000, 0x00000000},
		{" ", 0x00000000, 0x7ef49b98},
		{"t", 0x00000000, 0xca87df4d},
		{"te", 0x00000000, 0xedb8ee1b},
		{"tes", 0x00000000, 0x0bb90e5a},
		{"test", 0x00000000, 0xba6bd213},
		{"testy", 0x00000000, 0x44af8342},
		{"testy1", 0x00000000, 0x8a1a243a},
		{"testy12", 0x00000000, 0x845461b9},
		{"testy123", 0x00000000, 0x47628ac4},
		{"special characters açb↓c", 0x00000000, 0xbe83b140},
		{"The quick brown fox jumps over the lazy dog", 0x00000000, 0x2e4ff723},
		{"", 0xdeadbeef, 0x0de5c6a9},
		{" ", 0xdeadbeef, 0x25acce43},
		{"t", 0xdeadbeef, 0x3b15dcf8},
		{"te", 0xdeadbeef, 0xac981332},
		{"tes", 0xdeadbeef, 0xc1c78dda},
		{"test", 0xdeadbeef, 0xaa22d41a},
		{"testy", 0xdeadbeef, 0x84f5f623},
		{"testy1", 0xdeadbeef, 0x09ed28e9},
		{"testy12", 0xdeadbeef, 0x22467835},
		{"testy123", 0xdeadbeef, 0xd633060d},
		{"special characters açb↓c", 0xdeadbeef, 0xf7fdd8a2},
		{"The quick brown fox jumps over the lazy dog", 0xdeadbeef, 0x3a7b3f4d},
		{"", 0x00000001, 0x514e28b7},
		{" ", 0x00000001, 0x4f0f7132},
		{"t", 0x00000001, 0x5db1831e},
		{"te", 0x00000001, 0xd248bb2e},
		{"tes", 0x00000001, 0xd432eb74},
		{"test", 0x00000001, 0x99c02ae2},
		{"testy", 0x00000001, 0xc5b2dc1e},
		{"testy1", 0x00000001, 0x33925ceb},
		{"testy12", 0x00000001, 0xd92c9f23},
		{"testy123", 0x00000001, 0x3bc1712d},
		{"special characters açb↓c", 0x00000001, 0x293327b5},
		{"The quick brown fox jumps over the lazy dog", 0x00000001, 0x78e69e27},
	}
	a := &Assigner{}
	for _, c := range cases {
		assert.Equal(t, c.hash, a.hashMur([]byte(c.data), c.seed), "hash(%.32s, %x)", c.data, c.seed)
	}
}

// TestHashUnit checks full variant assignment. The sensitivity of tests may be low because of small variant number.
// Test cases are taken from:
// https://github.com/absmartly/javascript-sdk/blob/311acea8966a0db92b8b7996c4d4752125fe968b/src/__tests__/assigner.test.js
func TestAssigner(t *testing.T) {
	cases := map[string][]struct {
		split          []float64
		seedHi, seedLo uint32
		variant        int
	}{
		"bleh@absmartly.com": {
			{[]float64{0.5, 0.5}, 0x00000000, 0x00000000, 0},
			{[]float64{0.5, 0.5}, 0x00000000, 0x00000001, 1},
			{[]float64{0.5, 0.5}, 0x8015406f, 0x7ef49b98, 0},
			{[]float64{0.5, 0.5}, 0x3b2e7d90, 0xca87df4d, 0},
			{[]float64{0.5, 0.5}, 0x52c1f657, 0xd248bb2e, 0},
			{[]float64{0.5, 0.5}, 0x865a84d0, 0xaa22d41a, 0},
			{[]float64{0.5, 0.5}, 0x27d1dc86, 0x845461b9, 1},
			{[]float64{0.33, 0.33, 0.34}, 0x00000000, 0x00000000, 0},
			{[]float64{0.33, 0.33, 0.34}, 0x00000000, 0x00000001, 2},
			{[]float64{0.33, 0.33, 0.34}, 0x8015406f, 0x7ef49b98, 0},
			{[]float64{0.33, 0.33, 0.34}, 0x3b2e7d90, 0xca87df4d, 0},
			{[]float64{0.33, 0.33, 0.34}, 0x52c1f657, 0xd248bb2e, 0},
			{[]float64{0.33, 0.33, 0.34}, 0x865a84d0, 0xaa22d41a, 1},
			{[]float64{0.33, 0.33, 0.34}, 0x27d1dc86, 0x845461b9, 1},
		},
		"123456789": {
			{[]float64{0.5, 0.5}, 0x00000000, 0x00000000, 1},
			{[]float64{0.5, 0.5}, 0x00000000, 0x00000001, 0},
			{[]float64{0.5, 0.5}, 0x8015406f, 0x7ef49b98, 1},
			{[]float64{0.5, 0.5}, 0x3b2e7d90, 0xca87df4d, 1},
			{[]float64{0.5, 0.5}, 0x52c1f657, 0xd248bb2e, 1},
			{[]float64{0.5, 0.5}, 0x865a84d0, 0xaa22d41a, 0},
			{[]float64{0.5, 0.5}, 0x27d1dc86, 0x845461b9, 0},
			{[]float64{0.33, 0.33, 0.34}, 0x00000000, 0x00000000, 2},
			{[]float64{0.33, 0.33, 0.34}, 0x00000000, 0x00000001, 1},
			{[]float64{0.33, 0.33, 0.34}, 0x8015406f, 0x7ef49b98, 2},
			{[]float64{0.33, 0.33, 0.34}, 0x3b2e7d90, 0xca87df4d, 2},
			{[]float64{0.33, 0.33, 0.34}, 0x52c1f657, 0xd248bb2e, 2},
			{[]float64{0.33, 0.33, 0.34}, 0x865a84d0, 0xaa22d41a, 0},
			{[]float64{0.33, 0.33, 0.34}, 0x27d1dc86, 0x845461b9, 0},
		},
		"e791e240fcd3df7d238cfc285f475e8152fcc0ec": {
			{[]float64{0.5, 0.5}, 0x00000000, 0x00000000, 1},
			{[]float64{0.5, 0.5}, 0x00000000, 0x00000001, 0},
			{[]float64{0.5, 0.5}, 0x8015406f, 0x7ef49b98, 1},
			{[]float64{0.5, 0.5}, 0x3b2e7d90, 0xca87df4d, 1},
			{[]float64{0.5, 0.5}, 0x52c1f657, 0xd248bb2e, 0},
			{[]float64{0.5, 0.5}, 0x865a84d0, 0xaa22d41a, 0},
			{[]float64{0.5, 0.5}, 0x27d1dc86, 0x845461b9, 0},
			{[]float64{0.33, 0.33, 0.34}, 0x00000000, 0x00000000, 2},
			{[]float64{0.33, 0.33, 0.34}, 0x00000000, 0x00000001, 0},
			{[]float64{0.33, 0.33, 0.34}, 0x8015406f, 0x7ef49b98, 2},
			{[]float64{0.33, 0.33, 0.34}, 0x3b2e7d90, 0xca87df4d, 1},
			{[]float64{0.33, 0.33, 0.34}, 0x52c1f657, 0xd248bb2e, 0},
			{[]float64{0.33, 0.33, 0.34}, 0x865a84d0, 0xaa22d41a, 0},
			{[]float64{0.33, 0.33, 0.34}, 0x27d1dc86, 0x845461b9, 1},
		},
	}
	for unit, sub := range cases {
		for i, c := range sub {
			a := &Assigner{
				SeedHi: c.seedHi,
				SeedLo: c.seedLo,
				Split:  c.split,
			}
			variant, _ := a.Assign(unit)
			assert.Equal(t, c.variant, variant, "%s-%d", unit, i)
		}
	}
}

// TestProbability todo
func TestProbability(t *testing.T) {

}
