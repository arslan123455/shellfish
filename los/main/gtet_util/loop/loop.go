package loop

import (
	"runtime"

	"github.com/phil-mansfield/shellfish/render/io"
	rgeom "github.com/phil-mansfield/shellfish/render/geom"
	"github.com/phil-mansfield/shellfish/los/geom"
	intr "github.com/phil-mansfield/shellfish/math/interpolate"
	util "github.com/phil-mansfield/shellfish/los/main/gtet_util"
)

// InterpolatorType is the type of interpolation used during the iteration.
type InterpolatorType int

// UnitType indicates what type of geometric object is being iterated over.
type UnitType int

const (
	Cubic InterpolatorType = iota
	Linear

	Particle UnitType = iota
	Tetra
)

// An object which will be passed to Loop(). It must be less than half the
// size of the simulation box (because delaing with larger objects is a
// nightmare).
type Object interface {
	// Transform does a coordinate transformation on a slice of vectors such
	// that they are all as close to the center of the Object as possible.
	Transform(vecs []rgeom.Vec, tw float64)
	// Contains returns true if a point is contained inside the Object.
	Contains(x, y, z float64) bool
	// IntersectBox returns true if a box intersects the Object. This function
	// is allowed to be slow.
	IntersectBox(origin, span [3]float64, tw float64) bool

	// ThreadCopy copies enough of the object's internal state to a new instance
	// of that object so that both Objects can maintain calls to Use*foo*
	// simultaneously.
	ThreadCopy(id, threads int) Object
	// ThreadMerge combines the data from a group of objects into the reciever.
	// The reciever will _not_ be in the object list, but its initial data
	// should be combined likewise.
	ThreadMerge(objs []Object)

	// UsePoint supplies a point to the object.
	UsePoint(x, y, z float64)
	// UseTetra supplies a tetrahedron to the object.
	UseTetra(t *geom.Tetra)

	// Params returns miscellaneous parameters.
	Params() Params
}

// Params contains parameters that 
type Params struct {
	// UsesTetra is true if tetrahedra should be iterated over and false if
	// points should be iterated over.
	UsesTetra bool
}

// Buffer is a struct which contains various pre-allocated buffers
type Buffer struct {
	vecs []rgeom.Vec
	ib *intrBuffers
}

// NewBuffer allocated a new Buffer struct.
func NewBuffer() *Buffer {
	return &Buffer{}
}

func (b *Buffer) read(hd *io.SheetHeader, file string, skip, pts int) error {
	n := int(hd.GridWidth*hd.GridWidth*hd.GridWidth)
	if n > len(b.vecs) {
		b.vecs = make([]rgeom.Vec, n)
		b.ib = newIntrBuffers(int(hd.SegmentWidth),
			int(hd.GridWidth), skip, pts)
	} else if n < len(b.vecs) {
		b.vecs = b.vecs[:n]
		b.ib = newIntrBuffers(int(hd.SegmentWidth),
			int(hd.GridWidth), skip, pts)
	}

	return io.ReadSheetPositionsAt(file, b.vecs)
}

// Loop iterates over all the tetrahedra or all the points in the simulation
// box. A lot of tricks are used to minimize the number of operations done.
//
// TODO: restructure so there are fewer function arguments here.
func Loop(snap int, objs []Object, buf *Buffer, skip int,
	it InterpolatorType, pts, workers int) error {
	
	runtime.GC()

	if workers != 1 {
		panic("Cannot support multiple workers yet.")
	}

	hds, files, err := util.ReadHeaders(snap)
	if err != nil { return err }

	// Allocate needed buffer space.
	con := newIntrConstructor(it)

	// Loop over all the segments and objects that intersect with one
	// another
	intrBins := binHeaderIntersections(hds, objs)
	for i := range hds {
		for _, j := range intrBins[i] {
			err = buf.read(&hds[i], files[i], skip, pts)
			if err != nil { return err }

			// Loop over all the points in a particular segment.
			loopSegment(buf.vecs, pts, objs[j], con, buf.ib, hds[i].TotalWidth)
		}
	}

	return nil
}

// intrConstructor creates a new 3D interpolator.
type intrConstructor func(
	float64, float64, int,
	float64, float64, int,
	float64, float64, int, []float64) intr.TriInterpolator

// intrBuffers contains the space required for interpolating.
type intrBuffers struct {
	gw, sw, kw, skip int
	xs, ys, zs []float64
	vecIntr, boxIntr []bool

	// Only one of these two ever actually has data in it.
	layer layer
	tetraLayers tetraLayers
}

type layer struct {
	x, y, z intr.TriInterpolator
}

type tetraLayers struct {
	low, high layer
	lowPts, highPts [][]rgeom.Vec
	fullPts [][]rgeom.Vec // lowPts and highPts are slices of fullPts.
}

//////////////////
// Constructors //
//////////////////

// newIntrConstructor returns a function pointer to a TriInterpolator
// constructor.
func newIntrConstructor(it InterpolatorType) intrConstructor {
	switch it {
	case Linear:
		return func(x0, dx float64, nx int,
			y0, dy float64, ny int, 
			z0, dz float64, nz int, vals []float64) intr.TriInterpolator {
				return intr.NewUniformTriLinear(
					x0, dx, nx, y0, dy, ny, z0, dz, nz, vals,
				)
			}
	case Cubic:
		return func(x0, dx float64, nx int,
			y0, dy float64, ny int, 
			z0, dz float64, nz int, vals []float64) intr.TriInterpolator {
				return intr.NewUniformTriCubic(
					x0, dx, nx, y0, dy, ny, z0, dz, nz, vals,
				)
			}
	}
	panic(":3")
}


// newIntrBuffers allocates a new set of buffers for a given set of grid
// parameters.
func newIntrBuffers(segWidth, gridWidth, skip, pts int) *intrBuffers {
	buf := &intrBuffers{}
	buf.gw = gridWidth // Remember! This is the bigger one!!!
	buf.sw = segWidth
	buf.skip = skip
	buf.kw = (segWidth/skip) + 1

	buf.xs = make([]float64, buf.kw*buf.kw*buf.kw)
	buf.ys = make([]float64, len(buf.xs))
	buf.zs = make([]float64, len(buf.xs))
	
	buf.vecIntr = make([]bool, buf.kw*buf.kw*buf.kw)
	buf.boxIntr = make([]bool, (buf.kw-1)*(buf.kw-1)*(buf.kw-1))

	buf.tetraLayers.lowPts = make([][]rgeom.Vec, buf.kw - 1)
	buf.tetraLayers.highPts = make([][]rgeom.Vec, buf.kw - 1)
	buf.tetraLayers.fullPts = make([][]rgeom.Vec, buf.kw - 1)
	lowPts := buf.tetraLayers.lowPts
	highPts := buf.tetraLayers.highPts
	fullPts := buf.tetraLayers.fullPts
	n := (pts+1)*(pts+1)
	for i := range fullPts {
		fullPts[i] = make([]rgeom.Vec, 2*n)
		lowPts[i] = fullPts[i][:n]
		highPts[i] = fullPts[i][n:]
	}
	
	return buf
}

// loadBuffers inserts a set of vectors into the intrBuffers.
func (buf *intrBuffers) loadVecs(vecs []rgeom.Vec, obj Object, tw float64) {
	kw, gw, s := buf.kw, buf.gw, buf.skip

	obj.Transform(vecs, tw)

	// Construct per-vector buffers.
	ik := 0
	for zk := 0; zk < kw; zk++ {
		for yk := 0; yk < kw; yk++ {
			for xk := 0; xk < kw; xk++ {
				xg, yg, zg := xk*s, yk*s, zk*s
				ig := xg + yg*gw + zg*gw*gw
				x := float64(vecs[ig][0])
				y := float64(vecs[ig][1])
				z := float64(vecs[ig][2])
				buf.vecIntr[ik] = obj.Contains(x, y, z)

				buf.xs[ik] = x
				buf.ys[ik] = y
				buf.zs[ik] = z

				ik++
			}
		}
	}
	
	// Construct per-box buffers.
	v := buf.vecIntr
	i := 0
	for z0 := 0; z0 < kw-1; z0++ {
		z1 := z0 + 1
		for y0 := 0; y0 < kw-1; y0++ {
			y1 := y0 + 1
			for x0 := 0; x0 < kw-1; x0++ {
				x1 := x0 + 1
				
				i000 := x0 + y0*kw + z0*kw*kw
				i001 := x0 + y0*kw + z1*kw*kw
				i010 := x0 + y1*kw + z0*kw*kw
				i011 := x0 + y1*kw + z1*kw*kw

				i100 := x1 + y0*kw + z0*kw*kw
				i101 := x1 + y0*kw + z1*kw*kw
				i110 := x1 + y1*kw + z0*kw*kw
				i111 := x1 + y1*kw + z1*kw*kw
				
				buf.boxIntr[i] = v[i000] || v[i001] || v[i010] || v[i011] ||
					v[i100] || v[i101] || v[i110] || v[i111]
				
				i++
			}
		}
	}
}

////////////////////
// Load Balancing //
////////////////////

// Used for load balancing.
func (buf *intrBuffers) zCounts() []int {
	counts := make([]int, buf.kw-1)

	i := 0
	for z := 0; z < buf.kw-1; z++ {
		for y := 0; y < buf.kw-1; y++ {
			for x := 0; x < buf.kw-1; x++ {
				if buf.boxIntr[i] { counts[z]++ }
				i++
			}
		}
	}
	
	return counts
}

// Used for load balanacing.
func zSplit(zCounts []int, workers int) [][]int {
	tot := 0
	for _, n := range zCounts { tot += n }

	splits := make([]int, workers + 1)
	si := 1
	splitWidth := tot / workers
	if splitWidth * workers < tot { splitWidth++ }
	target := splitWidth

	sum := 0
	for i, n := range zCounts {
		sum += n
		if sum > target {
			splits[si] = i
			for sum > target { target += splitWidth }
			si++
		}
	}
	for ; si < len(splits); si++ { splits[si] = len(zCounts) }

	splitIdxs := make([][]int, workers)
	for i := range splitIdxs {
		jStart, jEnd := splits[i], splits[i + 1]
		for j := jStart; j < jEnd; j++ {
			if zCounts[j] > 0 { splitIdxs[i] = append(splitIdxs[i], j) }
		}
	}

	return splitIdxs
}

// loopSegment places the density field represented by the given
// points into the given profile.
func loopSegment(
	vecs []rgeom.Vec, pts int, obj Object,
	con intrConstructor, buf *intrBuffers,
	tw float64,
) {
	buf.loadVecs(vecs, obj, tw)

	// Yup... lots of allocations happening here... -___-
	// This could be improved.
	runtime.GC()

	// These should probably use an .init() method or something.
	if obj.Params().UsesTetra {
		low, high := &buf.tetraLayers.low, &buf.tetraLayers.high
		low.x = con(0, 1, buf.kw, 0, 1, buf.kw, 0, 1, buf.kw, buf.xs)
		low.y = con(0, 1, buf.kw, 0, 1, buf.kw, 0, 1, buf.kw, buf.ys)
		low.z = con(0, 1, buf.kw, 0, 1, buf.kw, 0, 1, buf.kw, buf.zs)
		high.x = con(0, 1, buf.kw, 0, 1, buf.kw, 0, 1, buf.kw, buf.xs)
		high.y = con(0, 1, buf.kw, 0, 1, buf.kw, 0, 1, buf.kw, buf.ys)
		high.z = con(0, 1, buf.kw, 0, 1, buf.kw, 0, 1, buf.kw, buf.zs)
	} else {
		buf.layer.x = con(0, 1, buf.kw, 0, 1, buf.kw, 0, 1, buf.kw, buf.xs)
		buf.layer.y = con(0, 1, buf.kw, 0, 1, buf.kw, 0, 1, buf.kw, buf.ys)
		buf.layer.z = con(0, 1, buf.kw, 0, 1, buf.kw, 0, 1, buf.kw, buf.zs)
	}
	
	xBuf := make([]int, 0, buf.kw*buf.kw)
	yBuf := make([]int, 0, buf.kw*buf.kw)
	
	i := 0
	for z := 0; z < buf.kw-1; z++ {
		xBuf := xBuf[0:0]
		yBuf := yBuf[0:0]
		for y := 0; y < buf.kw-1; y++ {
			for x := 0; x < buf.kw-1; x++ {
				if buf.boxIntr[i] {
					xBuf = append(xBuf, x)
					yBuf = append(yBuf, y)
				}
				i++
			}
		}
		
		if len(xBuf) > 0 {
			if obj.Params().UsesTetra {
				xyTetraInterpolate(xBuf, yBuf, z, buf.tetraLayers, pts, obj)
			} else {
				xyInterpolate(xBuf, yBuf, z, buf.layer, pts, obj)
			}
		}
	}
}

func xyInterpolate(
	xBuf, yBuf []int, zIdx int,
	layer layer, pts int, obj Object,
) {
	triX, triY, triZ := layer.x, layer.y, layer.z
	
	dp := 1 / float64(pts)
	z0 := float64(zIdx)

	// xl, yl, zl - Lagrangian values in code units.
	
	for zi := 0; zi < pts; zi++ {
		zl := z0 + float64(zi) * dp

		// Iterate over y indices.
		iStart, iEnd := 0, 0
		for iEnd < len(xBuf) {
			yIdx := yBuf[iStart]
			y0 := float64(yIdx)

			// Find the index range of the current yIdx.
			for iEnd = iStart; iEnd < len(xBuf); iEnd++ {
				if yBuf[iEnd] != yIdx { break }
			}

			for yi := 0; yi < pts; yi++ {
				yl := y0 + float64(yi) * dp
				// Iterate over x indices.
				for _, xIdx := range xBuf[iStart: iEnd] {
					x0 := float64(xIdx)
					for xi := 0; xi < pts; xi++ {
						xl := x0 + float64(xi) * dp

						x := triX.Eval(xl, yl, zl)
						y := triY.Eval(xl, yl, zl)
						z := triZ.Eval(xl, yl, zl)
						
						obj.UsePoint(x, y, z)
					}
				}
			}

			iStart = iEnd
		}
	}
}

// xyTetraInterpolate is the the interpolation kernel for tetrahedral iteration.
// It gets pretty scary in here so be care-Aaaarrrrggggh!!!
// ph'nglui mglw'nafh Cthulhu R'lyeh wgah'nagl fhtagn
func xyTetraInterpolate(
	xBuf, yBuf []int, zIdx int,
	tetraLayers tetraLayers, pts int, obj Object,
) {
	low, high := tetraLayers.low, tetraLayers.high
	lowPts, highPts := tetraLayers.lowPts, tetraLayers.highPts
	fullPts := tetraLayers.fullPts
	idxBuf, tet := &rgeom.TetraIdxs{}, &geom.Tetra{}
	
	dp := 1 / float64(pts)
	z0 := float64(zIdx)

	for zi := 0; zi < pts; zi++ {
		// xl, yl, zl - Lagrangian values in code units.
		zl := z0 + float64(zi) * dp

		// Iterate over y indices.
		iStart, iEnd := 0, 0
		for iEnd < len(xBuf) {
			yIdx := yBuf[iStart]
			y0 := float64(yIdx)

			// Find the index range of the current yIdx.
			for iEnd = iStart; iEnd < len(xBuf); iEnd++ {
				if yBuf[iEnd] != yIdx { break }
			}

			for yi := 0; yi < pts+1; yi++ {
				yl := y0 + float64(yi) * dp
				// Iterate over x indices. Save all the interpolated points
				// in the 
				for _, xIdx := range xBuf[iStart: iEnd] {
					x0 := float64(xIdx)
					for xi := 0; xi < pts+1; xi++ {
						xl := x0 + float64(xi) * dp

						// Interpolate both the low and the high layer. Place
						// those points into their respective point buffers.

						// Low layer
						x := low.x.Eval(xl, yl, zl)
						y := low.y.Eval(xl, yl, zl)
						z := low.z.Eval(xl, yl, zl)
						vec := rgeom.Vec{ float32(x), float32(y), float32(z) }
						ptIdx := xi + yi*(pts+1)
						lowPts[xIdx][ptIdx] = vec

						// High layer.
						zlh := z0 + float64(zi+1)*dp
						x = high.x.Eval(xl, yl, zlh)
						y = high.y.Eval(xl, yl, zlh)
						z = high.z.Eval(xl, yl, zlh)
						vec = rgeom.Vec{ float32(x), float32(y), float32(z) }
						highPts[xIdx][ptIdx] = vec
					}
				}
			}
			
			// Now loop over all the tetrahedra.
			for yi := 0; yi < pts; yi++ {
				for _, xIdx := range xBuf[iStart: iEnd] {
					ptBuf := fullPts[xIdx]
					for xi := 0; xi < pts; xi++ {
						idx := xi + yi*(pts+1)
						for dir := 0; dir < 6; dir++ {
							idxBuf.Init(int64(idx), int64(pts+1), 1, dir)
							tet[0] = geom.Vec(ptBuf[idxBuf[0]])
							tet[1] = geom.Vec(ptBuf[idxBuf[1]])
							tet[2] = geom.Vec(ptBuf[idxBuf[2]])
							tet[3] = geom.Vec(ptBuf[idxBuf[3]])
							obj.UseTetra(tet)
						}
					}
				}
			}
			
			iStart = iEnd
		}
		// We're moving up a layer, so use the old high spline as the new low
		// spline to prevent unneeded moving of the internal spline layer.
		high, low = low, high
	}
}
// You... you make it out alive! Now, run! Run far away from here!

func binHeaderIntersections(
	hds []io.SheetHeader, objs []Object,
) [][]int {
	bins := make([][]int, len(hds))
	for i := range hds {
		origin := vecTo64(hds[i].Origin)
		span := vecTo64(hds[i].Width)
		for j := range objs {
			if objs[j].IntersectBox(origin, span, hds[0].TotalWidth) {
				bins[i] = append(bins[i], j)
			}
		}
	}
	return bins
}

func vecTo64(v rgeom.Vec) [3]float64 {
	return [3]float64{ float64(v[0]), float64(v[1]), float64(v[2]) }
}
