package analyze

import (
	"math"
	"math/rand"

	"github.com/phil-mansfield/shellfish/math/sort"
)

type Shell func(phi, theta float64) float64

func randomAngle() (phi, theta float64) {
	u, v := rand.Float64(), rand.Float64()
	return 2 * math.Pi * u, math.Acos(2*v - 1)
}

func cartesian(phi, theta, r float64) (x, y, z float64) {
	sinP, cosP := math.Sincos(phi)
	sinT, cosT := math.Sincos(theta)
	return r * sinT * cosP, r * sinT * sinP, r * cosT
}

func (s Shell) CartesianSampledVolume(samples int, rMax float64) float64 {
	inside := 0
	for i := 0; i < samples; i++ {
		x := rand.Float64()*(2*rMax) - rMax
		y := rand.Float64()*(2*rMax) - rMax
		z := rand.Float64()*(2*rMax) - rMax

		r := math.Sqrt(x*x + y*y + z*z)
		phi := math.Atan2(y, x)
		th := math.Acos(z / r)

		rs := s(phi, th)
		if r < rs {
			inside++
		}
	}

	return float64(inside) / float64(samples) * (rMax * rMax * rMax * 8)
}

func (s Shell) Volume(samples int) float64 {
	sum := 0.0
	for i := 0; i < samples; i++ {
		phi, theta := randomAngle()
		r := s(phi, theta)
		sum += r * r * r
	}
	r3 := sum / float64(samples)
	return r3 * 4 * (math.Pi / 3)
}

func (s Shell) MeanRadius(samples int) float64 {
	sum := 0.0
	for i := 0; i < samples; i++ {
		phi, th := randomAngle()
		r := s(phi, th)
		sum += r
	}
	return sum / float64(samples)
}

func (s Shell) MedianRadius(samples int) float64 {
	rs := make([]float64, samples)
	for i := range rs {
		phi, th := randomAngle()
		rs[i] = s(phi, th)
	}
	return sort.Median(rs, rs)
}

func (s Shell) Moments(samples int) (Ix, Iy, Iz float64) {
	xSum, ySum, zSum, rSum := 0.0, 0.0, 0.0, 0.0
	for i := 0; i < samples; i++ {
		phi, theta := randomAngle()
		r := s(phi, theta)
		x, y, z := cartesian(phi, theta, r)
		xSum += (y*y + z*z) * r * r
		ySum += (x*x + z*z) * r * r
		zSum += (x*x + y*y) * r * r
		rSum += r * r
	}
	return xSum / rSum, ySum / rSum, zSum / rSum
}

func (s Shell) SurfaceArea(samples int) float64 {
	sum := 0.0
	for i := 0; i < samples; i++ {
		phi, theta := randomAngle()
		r := s(phi, theta)
		sum += r * r
	}
	return sum / float64(samples) * 4 * math.Pi
}

func (s1 Shell) DiffVolume(s2 Shell, samples int) float64 {
	sum := 0.0
	for i := 0; i < samples; i++ {
		phi, theta := randomAngle()
		r1, r2 := s1(phi, theta), s2(phi, theta)
		r := (r1 + r2) / 2
		dr := math.Abs(r1 - r2)
		sum += dr * r * r
	}
	return sum / float64(samples) * (4 * math.Pi) / 3
}

func (s1 Shell) MaxDiff(s2 Shell, samples int) float64 {
	max := 0.0
	for i := 0; i < samples; i++ {
		phi, theta := randomAngle()
		r1, r2 := s1(phi, theta), s2(phi, theta)
		dr := math.Abs(r1 - r2)
		if dr > max {
			max = dr
		}
	}
	return max
}

func (s Shell) RadialRange(samples int) (low, high float64) {
	phi, theta := randomAngle()
	low = s(phi, theta)
	high = low
	for i := 0; i < samples; i++ {
		phi, theta := randomAngle()
		r := s(phi, theta)
		if r > high {
			high = r
		}
		if r < low {
			low = r
		}
	}
	return low, high
}

func (s Shell) Contains(x, y, z float64) bool {
	r := math.Sqrt(x*x + y*y + z*z)
	phi := math.Atan2(y, x)
	theta := math.Acos(z / r)
	return s(phi, theta) > r
}

type Tracers struct {
	Vol, Sa, Ix, Iy, Iz float64
}

func CumulativeTracers(
	shells [][]Shell, samples int,
) (means, stds []Tracers) {
	rings := len(shells[0])
	sums, sqrs := make([]Tracers, rings), make([]Tracers, rings)

	for ir := range shells[0] {
		for ih := range shells {
			shell := shells[ih][ir]

			vol := shell.Volume(samples)
			sa := shell.SurfaceArea(samples)
			ix, iy, iz := shell.Moments(samples)

			sums[ir].Vol += vol
			sums[ir].Sa += sa
			sums[ir].Ix += ix
			sums[ir].Iy += iy
			sums[ir].Iz += iz

			sqrs[ir].Vol += vol * vol
			sqrs[ir].Sa += sa * sa
			sqrs[ir].Ix += ix * ix
			sqrs[ir].Iy += iy * iy
			sqrs[ir].Iz += iz * iz
		}
	}

	means, stds = make([]Tracers, rings), make([]Tracers, rings)
	n := float64(len(shells))
	for i := range means {
		means[i].Vol = sums[i].Vol / n
		means[i].Sa = sums[i].Sa / n
		means[i].Ix = sums[i].Ix / n
		means[i].Iy = sums[i].Iy / n
		means[i].Iz = sums[i].Iz / n

		stds[i].Vol = math.Sqrt(sqrs[i].Vol/n - means[i].Vol*means[i].Vol)
		stds[i].Sa = math.Sqrt(sqrs[i].Sa/n - means[i].Sa*means[i].Sa)
		stds[i].Ix = math.Sqrt(sqrs[i].Ix/n - means[i].Ix*means[i].Ix)
		stds[i].Iy = math.Sqrt(sqrs[i].Iy/n - means[i].Iy*means[i].Iy)
		stds[i].Iz = math.Sqrt(sqrs[i].Iz/n - means[i].Iz*means[i].Iz)
	}

	return means, stds
}
