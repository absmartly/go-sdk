package sdk

// Ported from the Java SDK's SwitchbackAssignerGoldenVectorsTest (java-sdk
// main 07807b5). testAssets/switchback/golden-vectors.json is a byte-for-byte
// copy of absmartly/abs 9bd2b5ca35
// lib/utils/src/test/resources/switchback/golden-vectors.json (git blob
// b314abeb9e97b35f911c7caacec90b8ff73b9d77), vendored through java-sdk
// core-api/src/test/resources/switchback/golden-vectors.json.

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"reflect"
	"strconv"
	"testing"
)

type switchbackVector struct {
	ClusterUnit         json.RawMessage `json:"clusterUnit"`
	PeriodIndex         string          `json:"periodIndex"`
	CycleIndex          string          `json:"cycleIndex"`
	Seed                string          `json:"seed"`
	ArmCount            int             `json:"armCount"`
	ExpectedVariant     int             `json:"expectedVariant"`
	ExpectedPermutation []int           `json:"expectedPermutation"`
}

func loadSwitchbackVectors(t *testing.T) (assign []switchbackVector, permutation []switchbackVector) {
	t.Helper()
	var raw, err = os.ReadFile("testAssets/switchback/golden-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors struct {
		AssignSwitchback       []switchbackVector `json:"assignSwitchback"`
		FisherYatesPermutation []switchbackVector `json:"fisherYatesPermutation"`
	}
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	return vectors.AssignSwitchback, vectors.FisherYatesPermutation
}

// vectorClusterUnit is the cluster unit as text: a JSON string as-is, a JSON
// integer as its decimal digits.
func vectorClusterUnit(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var number json.Number
	var decoder = json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err != nil {
		t.Fatalf("cluster unit %s: %v", raw, err)
	}
	if _, err := strconv.ParseInt(number.String(), 10, 64); err != nil {
		t.Fatalf("cluster unit %s is not an integer", raw)
	}
	return number.String()
}

func exactInt64(t *testing.T, text string) int64 {
	t.Helper()
	var value, err = strconv.ParseInt(text, 10, 64)
	if err != nil {
		t.Fatalf("%q: %v", text, err)
	}
	return value
}

func TestSwitchbackAssign_MatchesEveryGoldenVector(t *testing.T) {
	var cases, _ = loadSwitchbackVectors(t)
	expectInt(t, 882, len(cases))
	var armCounts = map[int]bool{}
	var boundary = 0
	for _, c := range cases {
		var unit = vectorClusterUnit(t, c.ClusterUnit)
		var period, seed = exactInt64(t, c.PeriodIndex), exactInt64(t, c.Seed)
		armCounts[c.ArmCount] = true
		if period == math.MinInt64 || period == math.MaxInt64 || seed == math.MinInt64 || seed == math.MaxInt64 {
			boundary++
		}
		var got, err = SwitchbackAssign(unit, period, seed, c.ArmCount)
		if err != nil || got != c.ExpectedVariant {
			t.Errorf("assign(%q, %d, %d, %d) = %d, %v; want %d", unit, period, seed, c.ArmCount, got, err, c.ExpectedVariant)
		}
	}
	if !armCounts[2] || !armCounts[3] || !armCounts[10] || boundary == 0 {
		t.Fatalf("arm counts %v, boundary cases %d", armCounts, boundary)
	}
}

func TestSwitchbackPermutation_MatchesEveryGoldenVector(t *testing.T) {
	var _, cases = loadSwitchbackVectors(t)
	expectInt(t, 504, len(cases))
	for _, c := range cases {
		var unit = vectorClusterUnit(t, c.ClusterUnit)
		var cycle, seed = exactInt64(t, c.CycleIndex), exactInt64(t, c.Seed)
		var got, err = SwitchbackPermutation(unit, cycle, seed, c.ArmCount)
		if err != nil || !reflect.DeepEqual(got, c.ExpectedPermutation) {
			t.Errorf("permutation(%q, %d, %d, %d) = %v, %v; want %v", unit, cycle, seed, c.ArmCount, got, err, c.ExpectedPermutation)
		}
	}
}

func TestSwitchbackAssign_EveryCycleCoversEveryArmExactlyOnce(t *testing.T) {
	for _, armCount := range []int{2, 3, 10} {
		for cycle := int64(-3); cycle < 20; cycle++ {
			var seen = make([]bool, armCount)
			for slot := 0; slot < armCount; slot++ {
				var variant, _ = SwitchbackAssign("store_42", cycle*int64(armCount)+int64(slot), 0x1234_5678_9abc, armCount)
				if seen[variant] {
					t.Fatalf("arm %d repeated in cycle %d of %d arms", variant, cycle, armCount)
				}
				seen[variant] = true
			}
		}
	}
}

func TestSwitchbackAssign_RejectsFewerThanTwoArms(t *testing.T) {
	if _, err := SwitchbackAssign("u", 0, 0, 1); err == nil {
		t.Fatal("assign accepted 1 arm")
	}
	if _, err := SwitchbackPermutation("u", 0, 0, 0); err == nil {
		t.Fatal("permutation accepted 0 arms")
	}
}

func TestSwitchbackFloorDiv(t *testing.T) {
	for _, c := range []struct{ dividend, divisor, want int64 }{
		{0, 3, 0}, {2, 3, 0}, {3, 3, 1}, {-1, 3, -1}, {-3, 3, -1}, {-4, 3, -2},
		{math.MinInt64, 3, -3074457345618258603}, {math.MaxInt64, 10, 922337203685477580},
		{math.MinInt64, 1, math.MinInt64}, {math.MaxInt64, 1, math.MaxInt64},
	} {
		if got := floorDiv(c.dividend, c.divisor); got != c.want {
			t.Errorf("floorDiv(%d, %d) = %d, want %d", c.dividend, c.divisor, got, c.want)
		}
	}
}

// The seed's low word is seedLo's 32 bits, never sign-extended into the high
// word.
func TestSwitchbackSeed(t *testing.T) {
	expectValue(t, uint64(0xfffffffb_fffffff9), switchbackSeed(-5, -7))
	expectValue(t, uint64(0x00000001_ffffffff), switchbackSeed(1, -1))
	expectValue(t, uint64(0x80000000_00000000), switchbackSeed(math.MinInt32, 0))
	expectValue(t, uint64(0x7fffffff_7fffffff), switchbackSeed(math.MaxInt32, math.MaxInt32))
}

// The step probability is u32(stepHash) / 0xffffffff, so a hash exactly on
// a bucket edge moves to the upper bucket, and the largest hash is clamped
// to i. The golden vectors do not land on an edge.
func TestSwitchbackSwapIndex_BucketEdges(t *testing.T) {
	for _, c := range []struct {
		hash uint32
		i    int
		want int
	}{
		{0, 1, 0}, {0x7fffffff, 1, 0}, {0x80000000, 1, 1}, {math.MaxUint32, 1, 1},
		{1431655765, 2, 1}, {1431655764, 2, 0}, {2863311530, 2, 2}, {math.MaxUint32, 2, 2},
		{858993459, 4, 1}, {3435973836, 4, 4}, {math.MaxUint32, 9, 9},
	} {
		if got := switchbackSwapIndex(c.hash, c.i); got != c.want {
			t.Errorf("swapIndex(%d, %d) = %d, want %d", c.hash, c.i, got, c.want)
		}
	}
}
