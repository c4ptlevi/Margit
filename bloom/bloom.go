package bloom

import (
	"math"
	"sync/atomic"
)

type Filter struct {
	bits []uint64
	m    uint64
	k    uint64
}

func New(n uint64, p float64) *Filter {
	n = max(n, 1)
	m := uint64(math.Ceil(-float64(n) * math.Log(p) / (math.Ln2 * math.Ln2)))
	k := max(uint64(math.Round(float64(m)/float64(n)*math.Ln2)), 1)
	words := (m + 63) / 64
	return &Filter{bits: make([]uint64, words), m: words * 64, k: k}
}

func (f *Filter) Add(key string) {
	h1, h2 := hashes(key)
	for i := uint64(0); i < f.k; i++ {
		idx := (h1 + i*h2) % f.m
		atomic.OrUint64(&f.bits[idx/64], 1<<(idx%64))
	}
}

func (f *Filter) Test(key string) bool {
	h1, h2 := hashes(key)
	for i := uint64(0); i < f.k; i++ {
		idx := (h1 + i*h2) % f.m
		if atomic.LoadUint64(&f.bits[idx/64])&(1<<(idx%64)) == 0 {
			return false
		}
	}
	return true
}

func hashes(key string) (uint64, uint64) {
	h := uint64(14695981039346656037)
	for i := 0; i < len(key); i++ {
		h ^= uint64(key[i])
		h *= 1099511628211
	}
	h1 := mix(h)
	return h1, mix(h1) | 1
}

func mix(z uint64) uint64 {
	z += 0x9e3779b97f4a7c15
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}
