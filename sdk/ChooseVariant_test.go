package sdk

import (
	"testing"
)

func TestChooseVariant(t *testing.T) {
	var assigner VariantAssigner
	assert(1, assigner.ChooseVariant([]float64{0.0, 1.0}, 0.0), t)
	assert(1, assigner.ChooseVariant([]float64{0.0, 1.0}, 0.5), t)
	assert(1, assigner.ChooseVariant([]float64{0.0, 1.0}, 1.0), t)

	assert(0, assigner.ChooseVariant([]float64{1.0, 0.0}, 0.0), t)
	assert(0, assigner.ChooseVariant([]float64{1.0, 0.0}, 0.5), t)
	assert(1, assigner.ChooseVariant([]float64{1.0, 0.0}, 1.0), t)

	assert(0, assigner.ChooseVariant([]float64{0.5, 0.5}, 0.0), t)
	assert(0, assigner.ChooseVariant([]float64{0.5, 0.5}, 0.25), t)
	assert(0, assigner.ChooseVariant([]float64{0.5, 0.5}, 0.4999999), t)
	assert(1, assigner.ChooseVariant([]float64{0.5, 0.5}, 0.5), t)
	assert(1, assigner.ChooseVariant([]float64{0.5, 0.5}, 0.50000001), t)
	assert(1, assigner.ChooseVariant([]float64{0.5, 0.5}, 0.75), t)
	assert(1, assigner.ChooseVariant([]float64{0.5, 0.5}, 1.0), t)

	assert(0, assigner.ChooseVariant([]float64{0.333, 0.333, 0.334}, 0.0), t)
	assert(0, assigner.ChooseVariant([]float64{0.333, 0.333, 0.334}, 0.25), t)
	assert(0, assigner.ChooseVariant([]float64{0.333, 0.333, 0.334}, 0.33299999), t)
	assert(1, assigner.ChooseVariant([]float64{0.333, 0.333, 0.334}, 0.333), t)
	assert(1, assigner.ChooseVariant([]float64{0.333, 0.333, 0.334}, 0.33300001), t)
	assert(1, assigner.ChooseVariant([]float64{0.333, 0.333, 0.334}, 0.5), t)
	assert(1, assigner.ChooseVariant([]float64{0.333, 0.333, 0.334}, 0.66599999), t)
	assert(2, assigner.ChooseVariant([]float64{0.333, 0.333, 0.334}, 0.666), t)
	assert(2, assigner.ChooseVariant([]float64{0.333, 0.333, 0.334}, 0.66600001), t)
	assert(2, assigner.ChooseVariant([]float64{0.333, 0.333, 0.334}, 0.75), t)
	assert(2, assigner.ChooseVariant([]float64{0.333, 0.333, 0.334}, 1), t)

	assert(1, assigner.ChooseVariant([]float64{0.0, 1.0}, 0.0), t)
	assert(1, assigner.ChooseVariant([]float64{0.0, 1.0}, 1.0), t)

}

func TestAssignmentsMatch(t *testing.T) {
	l := [][]float64{
		{0.5, 0.5},
		{0.5, 0.5},
		{0.5, 0.5},
		{0.5, 0.5},
		{0.5, 0.5},
		{0.5, 0.5},
		{0.5, 0.5},
		{0.33, 0.33, 0.34},
		{0.33, 0.33, 0.34},
		{0.33, 0.33, 0.34},
		{0.33, 0.33, 0.34},
		{0.33, 0.33, 0.34},
		{0.33, 0.33, 0.34},
		{0.33, 0.33, 0.34},
	}

	s := [][]uint32{
		{uint32(0x00000000), uint32(0x00000000)},
		{uint32(0x00000000), uint32(0x00000001)},
		{uint32(0x8015406f), uint32(0x7ef49b98)},
		{uint32(0x3b2e7d90), uint32(0xca87df4d)},
		{uint32(0x52c1f657), uint32(0xd248bb2e)},
		{uint32(0x865a84d0), uint32(0xaa22d41a)},
		{uint32(0x27d1dc86), uint32(0x845461b9)},
		{uint32(0x00000000), uint32(0x00000000)},
		{uint32(0x00000000), uint32(0x00000001)},
		{uint32(0x8015406f), uint32(0x7ef49b98)},
		{uint32(0x3b2e7d90), uint32(0xca87df4d)},
		{uint32(0x52c1f657), uint32(0xd248bb2e)},
		{uint32(0x865a84d0), uint32(0xaa22d41a)},
		{uint32(0x27d1dc86), uint32(0x845461b9)},
	}

	assign(HashUnit("bleh@absmartly.com"), l, s,
		[]int{0, 1, 0, 0, 0, 0, 1, 0, 2, 0, 0, 0, 1, 1}, t)
	assign(HashUnit("123456789"), l, s,
		[]int{1, 0, 1, 1, 1, 0, 0, 2, 1, 2, 2, 2, 0, 0}, t)
	assign(HashUnit("e791e240fcd3df7d238cfc285f475e8152fcc0ec"), l, s,
		[]int{1, 0, 1, 1, 0, 0, 0, 2, 0, 2, 1, 0, 0, 1}, t)
}

func assign(hash []int8, l [][]float64, s [][]uint32, e []int, t *testing.T) {
	var assigner = NewVariantAssigner(hash)
	var buffer = [12]int8{}

	for i := 0; i < len(s); i++ {
		var frags = s[i]
		var split = l[i]
		var variant = assigner.Assign(split, int(frags[0]), int(frags[1]), buffer[:])
		assert(e[i], variant, t)
	}
}

func assert(want int, got int, t *testing.T) {
	if got != want {
		t.Errorf("got %q, wanted %q", got, want)
	}
}
