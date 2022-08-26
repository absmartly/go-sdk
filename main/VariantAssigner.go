package main

const normalizer float64 = 1.0 / 0xFFFFFFFF

type VariantAssigner struct {
	unitHash_ int
}

func NewVariantAssigner(hash []int8) *VariantAssigner {
	v := new(VariantAssigner)
	v.unitHash_ = Digest(hash, 0)
	return v
}

func (v VariantAssigner) ChooseVariant(split []float64, prob float64) int {
	var cumSum = 0.0

	for i := 0; i < len(split); i++ {
		cumSum += split[i]
		if prob < cumSum {
			return i
		}
	}
	return len(split) - 1
}

func (v VariantAssigner) Assign(split []float64, seedHi int, seedLo int, buffer []int8) int {
	var prob = v.probability(seedHi, seedLo, buffer)
	return v.ChooseVariant(split, prob)
}

func (v VariantAssigner) probability(seedHi int, seedLo int, buffer []int8) float64 {

	PutUInt32(buffer, 0, seedLo)
	PutUInt32(buffer, 4, seedHi)
	PutUInt32(buffer, 8, v.unitHash_)

	var hash = Digest(buffer, 0)
	return float64(hash&0xFFFFFFFF) * normalizer
}
