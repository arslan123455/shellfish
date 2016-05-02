package los

import (
	"testing"
)

func readDerivs(p *ProfileRing, i int) []float64 {
	out := make([]float64, p.bins)
	for j := 0; j < p.bins; j++ {
		out[j] = p.derivs[j+p.bins*i]
	}
	return out
}

func sliceAlmostEq(xs, ys []float64) bool {
	if len(xs) != len(ys) {
		return false
	}
	eps := 1e-5
	for i := range xs {
		if xs[i]+eps < ys[i] || xs[i]-eps > ys[i] {
			return false
		}
	}
	return true
}

func TestProfileRingInsert(t *testing.T) {
	n := 4
	bins := 10
	minR, maxR := 1.0, 2.0

	table := []struct {
		start, end, rho float64
		out             []float64
	}{
		{-1, 0, 1, []float64{0, 0, 0, 0, 0, 0, 0, 0, 0, 0}},
		{-1, 1, 1, []float64{0, 0, 0, 0, 0, 0, 0, 0, 0, 0}},
		{3, 4, 1, []float64{0, 0, 0, 0, 0, 0, 0, 0, 0, 0}},
		{2, 4, 1, []float64{0, 0, 0, 0, 0, 0, 0, 0, 0, 0}},
		{0, 3, 1, []float64{1, 0, 0, 0, 0, 0, 0, 0, 0, 0}},
		{0, 3, 10, []float64{10, 0, 0, 0, 0, 0, 0, 0, 0, 0}},
		{1.2, 3, 1, []float64{0, 0, 1, 0, 0, 0, 0, 0, 0, 0}},
		{1.25, 3, 2, []float64{0, 0, 1, 1, 0, 0, 0, 0, 0, 0}},
		{1.36, 3, 1, []float64{0, 0, 0, 0.4, 0.6, 0, 0, 0, 0, 0}},
		{0, 1.2, 1, []float64{1, 0, -1, 0, 0, 0, 0, 0, 0, 0}},
		{0, 1.25, 1, []float64{1, 0, -0.5, -0.5, 0, 0, 0, 0, 0, 0}},
		{0, 1.27, 1, []float64{1, 0, -0.3, -0.7, 0, 0, 0, 0, 0, 0}},
		{1.2, 1.3, 1, []float64{0, 0, 1, -1, 0, 0, 0, 0, 0, 0}},
		{1.24, 1.26, 1, []float64{0, 0, 0.2, -0.2, 0, 0, 0, 0, 0, 0}},
	}

	p := new(ProfileRing)

	for i, line := range table {
		p.Init(minR, maxR, bins, n)

		p.Insert(line.start, line.end, line.rho, i%n)
		res := readDerivs(p, i%n)
		if !sliceAlmostEq(res, line.out) {
			t.Errorf(
				"%d) Expected out = %v from start = %g end = %g. Got out = %v.",
				i, line.out, line.start, line.end, res,
			)
		}
	}
}

func TestProfileRingRetreive(t *testing.T) {
	n := 4
	bins := 10
	minR, maxR := 1.0, 2.0

	table := []struct {
		start, end, rho float64
		out             []float64
	}{
		{-1, 0, 1, []float64{0, 0, 0, 0, 0, 0, 0, 0, 0, 0}},
		{-1, 1, 1, []float64{0, 0, 0, 0, 0, 0, 0, 0, 0, 0}},
		{3, 4, 1, []float64{0, 0, 0, 0, 0, 0, 0, 0, 0, 0}},
		{2, 4, 1, []float64{0, 0, 0, 0, 0, 0, 0, 0, 0, 0}},
		{0, 3, 1, []float64{1, 1, 1, 1, 1, 1, 1, 1, 1, 1}},
		{0, 3, 10, []float64{10, 10, 10, 10, 10, 10, 10, 10, 10, 10}},
		{1.2, 3, 1, []float64{0, 0, 1, 1, 1, 1, 1, 1, 1, 1}},
		{1.25, 3, 2, []float64{0, 0, 1, 2, 2, 2, 2, 2, 2, 2}},
		{1.36, 3, 1, []float64{0, 0, 0, 0.4, 1, 1, 1, 1, 1, 1}},
		{0, 1.2, 1, []float64{1, 1, 0, 0, 0, 0, 0, 0, 0, 0}},
		{0, 1.25, 1, []float64{1, 1, 0.5, 0, 0, 0, 0, 0, 0, 0}},
		{0, 1.27, 1, []float64{1, 1, 0.7, 0, 0, 0, 0, 0, 0, 0}},
		{1.2, 1.3, 1, []float64{0, 0, 1, 0, 0, 0, 0, 0, 0, 0}},
		{1.24, 1.26, 1, []float64{0, 0, 0.2, 0, 0, 0, 0, 0, 0, 0}},
	}

	p := new(ProfileRing)
	for i, line := range table {
		p.Init(minR, maxR, bins, n)
		out := make([]float64, bins)

		p.Insert(line.start, line.end, line.rho, i%n)
		p.Retrieve(i%4, out)
		if !sliceAlmostEq(out, line.out) {
			t.Errorf(
				"%d) Expected out = %v from start = %g end = %g. Got out = %v.",
				i, line.out, line.start, line.end, out,
			)
		}
	}
}

func BenchmarkProfileRingInsert(b *testing.B) {
	n := 1000
	bins := 300
	minR, maxR := 0.05, 0.75

	p := new(ProfileRing)
	p.Init(minR, maxR, bins, n)

	m := 2089
	starts := make([]float64, m)
	ends := make([]float64, m)

	idx := 0
	pIdx := 0
	for i := 0; i < b.N; i++ {
		p.Insert(starts[idx], ends[idx], 1, pIdx)
		idx++
		pIdx++
		if idx == m {
			idx = 0
		}
		if pIdx == n {
			pIdx = 0
		}
	}
}
