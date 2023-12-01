package assigner

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/binary"
	"math"

	"github.com/spaolacci/murmur3"
)

type Assigner struct {
	seedHi uint32
	seedLo uint32
	split  []float64
}

func New(seedHi, seedLo uint32, split []float64) *Assigner {
	a := &Assigner{
		seedHi: seedHi,
		seedLo: seedLo,
		split:  split,
	}

	return a
}

func (a *Assigner) Assign(unit string) (int, string) {
	hu := a.hashUnit(unit)
	prob := a.probability(hu)
	var sum float64

	for i := 0; i < len(a.split); i++ {
		sum += a.split[i]
		if prob < sum {
			return i, hu
		}
	}

	return len(a.split) - 1, hu
}

func (a *Assigner) probability(unit string) float64 {
	uh := a.hashMur([]byte(unit), 0)
	var buff [12]byte
	binary.LittleEndian.PutUint32(buff[:], a.seedLo)
	binary.LittleEndian.PutUint32(buff[4:], a.seedHi)
	binary.LittleEndian.PutUint32(buff[8:], uh)
	h := a.hashMur(buff[:], 0)

	return float64(h) / math.MaxUint32
}

func (a *Assigner) hashUnit(unit string) string {
	hash := md5.Sum([]byte(unit))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

func (a *Assigner) hashMur(data []byte, seed uint32) uint32 {
	return murmur3.Sum32WithSeed(data, seed)
}
