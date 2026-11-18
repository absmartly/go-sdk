package sdk

import (
	"errors"
	"math"
	"strconv"
)

// Balanced-block switchback assignment. Port of the platform's
// SwitchbackAssignment (Guava murmur3_32_fixed, seed 0) and of the Java SDK's
// SwitchbackAssigner:
//
//	key       = utf8(clusterUnit + "\u001f" + cycleIndex)
//	baseHash  = murmur3_32(le64(seed) ++ le32(murmur3_32(key)))
//	stepHash  = murmur3_32(le32(baseHash) ++ le32(i)),  i = armCount-1 .. 1
//	j         = min(i, floor(u32(stepHash) / 0xffffffff * (i + 1)))
//	variant   = permutation[floorMod(periodIndex, armCount)], cycle = floorDiv(periodIndex, armCount)
//
// Any change here breaks parity with the platform golden vectors and the
// analysis-side schedule.

var errSwitchbackArmCount = errors.New("armCount must be >= 2")

const switchbackKeySeparator = "\u001f"

// switchbackNormalizer is Java's 1.0 / 0xffffffffL. The step probability is
// a product with it, not a quotient, so it rounds as the platform does.
const switchbackNormalizer float64 = 1.0 / math.MaxUint32

// SwitchbackAssign returns the variant a switchback cluster gets in a period:
// the balanced-block draw over the raw cluster value (never hashed),
// periodIndex = floorDiv(now - switchbackStartAt, switchbackPeriodMs) and the
// 64-bit seed (seedHi << 32) | (seedLo & 0xffffffff). It fails when armCount
// is less than 2.
func SwitchbackAssign(clusterUnit string, periodIndex int64, seed int64, armCount int) (int, error) {
	if armCount < 2 {
		return 0, errSwitchbackArmCount
	}
	return switchbackAssign(clusterUnit, periodIndex, uint64(seed), armCount), nil
}

// SwitchbackPermutation returns the arm order of a cluster's balanced block
// for a cycle: the variants periods cycleIndex*armCount .. +armCount-1 get.
// It fails when armCount is less than 2.
func SwitchbackPermutation(clusterUnit string, cycleIndex int64, seed int64, armCount int) ([]int, error) {
	if armCount < 2 {
		return nil, errSwitchbackArmCount
	}
	return switchbackPermutation(armCount, switchbackBaseHash(clusterUnit, cycleIndex, uint64(seed))), nil
}

func switchbackAssign(clusterUnit string, periodIndex int64, seed uint64, armCount int) int {
	var arms = int64(armCount)
	var cycleIndex = floorDiv(periodIndex, arms)
	// floorMod; the product may wrap near math.MinInt64, but the difference
	// is exact modulo 2^64.
	var cycleSlot = periodIndex - cycleIndex*arms
	return switchbackPermutation(armCount, switchbackBaseHash(clusterUnit, cycleIndex, seed))[cycleSlot]
}

// switchbackSeed is the 64-bit draw seed: seedHi in the high word and the
// low 32 bits of seedLo in the low word.
func switchbackSeed(seedHi int, seedLo int) uint64 {
	return uint64(uint32(seedHi))<<32 | uint64(uint32(seedLo))
}

func switchbackBaseHash(clusterUnit string, cycleIndex int64, seed uint64) uint32 {
	var key = clusterUnit + switchbackKeySeparator + strconv.FormatInt(cycleIndex, 10)
	var keyBytes = make([]int8, len(key)*3)
	var keyLen = EncodeUTF8(keyBytes, 0, key)
	var keyHash = DigestOffset(keyBytes, 0, keyLen, 0)

	var buffer [12]int8
	PutUInt32(buffer[:], 0, int(uint32(seed)))
	PutUInt32(buffer[:], 4, int(uint32(seed>>32)))
	PutUInt32(buffer[:], 8, keyHash)
	return uint32(Digest(buffer[:], 0))
}

func switchbackPermutation(armCount int, baseHash uint32) []int {
	var permutation = make([]int, armCount)
	for i := range permutation {
		permutation[i] = i
	}

	var buffer [8]int8
	PutUInt32(buffer[:], 0, int(baseHash))
	for i := armCount - 1; i > 0; i-- {
		PutUInt32(buffer[:], 4, i)
		var j = switchbackSwapIndex(uint32(Digest(buffer[:], 0)), i)
		permutation[i], permutation[j] = permutation[j], permutation[i]
	}
	return permutation
}

// switchbackSwapIndex is the Fisher-Yates step: the index in [0, i] a step
// hash selects.
func switchbackSwapIndex(stepHash uint32, i int) int {
	var prob = float64(stepHash) * switchbackNormalizer
	// prob == 1.0 would map to i + 1.
	var j = int(math.Floor(prob * float64(i+1)))
	if j > i {
		j = i
	}
	return j
}

// floorDiv rounds the quotient towards negative infinity; divisor is always
// positive here.
func floorDiv(dividend int64, divisor int64) int64 {
	var q = dividend / divisor
	if dividend%divisor != 0 && (dividend < 0) != (divisor < 0) {
		q--
	}
	return q
}
