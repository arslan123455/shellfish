package main

import (
	"flag"
	"log"
	"math"
	"runtime"
	"sort"

	"github.com/phil-mansfield/shellfish/cosmo"
	"github.com/phil-mansfield/shellfish/render/io"
	"github.com/phil-mansfield/shellfish/render/halo"
	"github.com/phil-mansfield/shellfish/los/geom"
	"github.com/phil-mansfield/shellfish/los/analyze"
	util "github.com/phil-mansfield/shellfish/los/main/gtet_util"
	
)

type Params struct {
	MaxMult float64
	RadOnly bool
}

func main() {
	p := parseCmd()
	ids, snaps, coeffs, err := util.ParseStdin()
	if err != nil { log.Fatal(err.Error()) }
	snapBins, coeffBins, idxBins := binBySnap(snaps, ids, coeffs)

	masses := make([]float64, len(ids))
	rads := make([]float64, len(ids))
	rmins := make([]float64, len(ids))
	rmaxes := make([]float64, len(ids))

	sortedSnaps := []int{}
	for snap := range snapBins {
		sortedSnaps = append(sortedSnaps, snap)
	}
	sort.Ints(sortedSnaps)

	log.Println("gtet_mass")
	for _, snap := range sortedSnaps {
		snapIDs := snapBins[snap]
		snapCoeffs := coeffBins[snap]
		idxs := idxBins[snap]

		for j := range idxs {
			rads[idxs[j]] = rSp(snapCoeffs[j])
			rmins[idxs[j]], rmaxes[idxs[j]] = rangeSp(snapCoeffs[j])
		}
		if p.RadOnly { continue }
		
		hds, files, err := util.ReadHeaders(snap)
		if err != nil { log.Fatal(err.Error()) }
		hBounds, err := boundingSpheres(snap, &hds[0], snapIDs, p)
		if err != nil { log.Fatal(err.Error()) }
		intrBins := binIntersections(hds, hBounds)

		rLows := make([]float64, len(snapCoeffs))
		rHighs := make([]float64, len(snapCoeffs))
		for i := range snapCoeffs {
			order := findOrder(snapCoeffs[i])
			shell := analyze.PennaFunc(snapCoeffs[i], order, order, 2)
			rLows[i], rHighs[i] = shell.RadialRange(10 * 1000)
		}

		xs := [][3]float32{}
		for i := range hds {
			if len(intrBins[i]) == 0 { continue }
			hd := &hds[i]

			n := hd.GridWidth*hd.GridWidth*hd.GridWidth
			if len(xs) == 0 { xs = make([][3]float32, n) }
			err := io.ReadSheetPositionsAt(files[i], xs)
			if err != nil { log.Fatal(err.Error()) }

			for j := range idxs {
				masses[idxs[j]] += massContained(
					&hds[i], xs, snapCoeffs[j],
					hBounds[j], rLows[j], rHighs[j],
				)
			}
		}
	}

	util.PrintCols(ids, snaps, masses, rads, rmins, rmaxes)
}

func parseCmd() *Params {
	p := &Params{}
	flag.Float64Var(&p.MaxMult, "MaxMult", 3, 
		"Ending radius of LoSs as a multiple of R_200m. " + 
			"Should be the same value as used in gtet_shell.")
	flag.BoolVar(&p.RadOnly, "RadOnly", false,
		"Only compute radii, not masses.")
	flag.Parse()
	return p
}

func binBySnap(
	snaps, ids []int, coeffs [][]float64,
) (snapBins map[int][]int,coeffBins map[int][][]float64,idxBins map[int][]int) {
	snapBins = make(map[int][]int)
	coeffBins = make(map[int][][]float64)
	idxBins = make(map[int][]int)
	for i, snap := range snaps {
		snapBins[snap] = append(snapBins[snap], ids[i])
		coeffBins[snap] = append(coeffBins[snap], coeffs[i])
		idxBins[snap] = append(idxBins[snap], i)
	}
	return snapBins, coeffBins, idxBins
}

func wrapDist(x1, x2, width float32) float32 {
	dist := x1 - x2
	if dist > width / 2 {
		return dist - width
	} else if dist < width / -2 {
		return dist + width
	} else {
		return dist
	}
}

func inRange(x, r, low, width, tw float32) bool {
	return wrapDist(x, low, tw) > -r && wrapDist(x, low + width, tw) < r
}

// SheetIntersect returns true if the given halo and sheet intersect one another
// and false otherwise.
func sheetIntersect(s geom.Sphere, hd *io.SheetHeader) bool {
	tw := float32(hd.TotalWidth)
	return inRange(s.C[0], s.R, hd.Origin[0], hd.Width[0], tw) &&
		inRange(s.C[1], s.R, hd.Origin[1], hd.Width[1], tw) &&
		inRange(s.C[2], s.R, hd.Origin[2], hd.Width[2], tw)
}

func binIntersections(
	hds []io.SheetHeader, spheres []geom.Sphere,
) [][]geom.Sphere {
	bins := make([][]geom.Sphere, len(hds))
	for i := range hds {
		for si := range spheres {
			if sheetIntersect(spheres[si], &hds[i]) {
				bins[i] = append(bins[i], spheres[si])
			}
		}
	}
	return bins
}

func boundingSpheres(
	snap int, hd *io.SheetHeader, ids []int, p *Params,
) ([]geom.Sphere, error) {
	vals, err := util.ReadRockstar(
		snap, ids, halo.X, halo.Y, halo.Z, halo.Rad200b,
	)

	if err != nil { return nil, err }
	xs, ys, zs, rs := vals[0], vals[1], vals[2], vals[3]

	spheres := make([]geom.Sphere, len(ids))
	for i := range spheres {
		spheres[i].C = [3]float32{
			float32(xs[i]), float32(ys[i]), float32(zs[i]),
		}
		spheres[i].R = float32(rs[i])
	}

	return spheres, nil
}

func findOrder(coeffs []float64) int {
	i := 1
	for {
		if 2*i*i == len(coeffs) {
			return i
		} else if 2*i*i > len(coeffs) {
			panic("Impossible")
		}
		i++
	}
}

func wrap(x, tw2 float32) float32 {
	if x > tw2 {
		return x - tw2
	} else if x < -tw2 {
		return x + tw2
	}
	return x
}

func coords(idx, cells int64) (x, y, z int64) {
    x = idx % cells
    y = (idx % (cells * cells)) / cells
    z = idx / (cells * cells)
    return x, y, z
}

func rSp(coeffs []float64) float64 {
	order := findOrder(coeffs)
	shell := analyze.PennaFunc(coeffs, order, order, 2)
	vol := shell.Volume(10 * 1000)
	r := math.Pow(vol / (math.Pi * 4 / 3), 0.33333)
	return r
	//return shell.MedianRadius(10 * 1000)
}

func rangeSp(coeffs []float64) (rmin, rmax float64) {
	order := findOrder(coeffs)
	shell := analyze.PennaFunc(coeffs, order, order, 2)
	return shell.RadialRange(10 * 1000)
}

func massContained(
	hd *io.SheetHeader, xs [][3]float32, coeffs []float64,
	sphere geom.Sphere, rLow, rHigh float64,
) float64 {
	sw := hd.SegmentWidth

	
	cpu := runtime.NumCPU()
	workers := int64(runtime.GOMAXPROCS(cpu))
	n := (sw*sw*sw) / workers
	outChan := make(chan float64, workers)
	for i := int64(0); i < workers - 1; i++ {
		go massContainedChan(
			hd, xs, coeffs, sphere, rLow, rHigh, n*i, n*(i+1), outChan,
		)
	}

	massContainedChan(
		hd, xs, coeffs, sphere, rLow, rHigh,
		n*(workers - 1), sw*sw*sw, outChan,
	)

	sum := 0.0
	for i := int64(0); i < workers; i++ {
		sum += <-outChan
	}
	
	return sum
}

func massContainedChan(
	hd *io.SheetHeader, xs [][3]float32, coeffs []float64,
	sphere geom.Sphere, rLow, rHigh float64,
	start, end int64, out chan float64,
) {
	c := &hd.Cosmo
	rhoM := cosmo.RhoAverage(c.H100 * 100, c.OmegaM, c.OmegaL, c.Z )
	dx := hd.TotalWidth / float64(hd.CountWidth) / (1 + c.Z)
	ptMass := rhoM * (dx*dx*dx)
	tw2 := float32(hd.TotalWidth) / 2

	order := findOrder(coeffs)
	shell := analyze.PennaFunc(coeffs, order, order, 2)
	low2, high2 := float32(rLow*rLow), float32(rHigh*rHigh)

	sum := 0.0
	sw := hd.SegmentWidth
	for si := start; si < end; si++ {
		xi, yi, zi := coords(si, hd.SegmentWidth)
		i := xi + yi*sw + zi*sw*sw
		x, y, z := xs[i][0], xs[i][1], xs[i][2]
		x, y, z = x - sphere.C[0], y - sphere.C[1], z - sphere.C[2]
		x = wrap(x, tw2)
		y = wrap(y, tw2)
		z = wrap(z, tw2)

		r2 := x*x + y*y +z*z

		if r2 < low2 || ( r2 < high2 &&
			shell.Contains(float64(x), float64(y), float64(z))) {
			sum += ptMass
		}
	}
	out <- sum
}
