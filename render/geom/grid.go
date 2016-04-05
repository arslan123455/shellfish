package geom

// Grid provides an interface for reasoning over a 1D slice as if it were a
// 3D grid.
type Grid struct {
	CellBounds
	Length, Area, Volume int
	uBounds [3]int
}

// GridLocation is a Grid which also specifies a physical location within
// a periodic super-grid.
type GridLocation struct {
	Grid
	Cells int
	BoxWidth float64
}

// CellBounds represents a bounding box aligned to grid cells.
type CellBounds struct {
	Origin, Width [3]int
}

// NewGrid returns a new Grid instance.
func NewGrid(origin, width [3]int) *Grid {
	g := &Grid{}
	g.Init(origin, width)
	return g
}

// Init initializes a Grid instance.
func (g *Grid) Init(origin, width [3]int) {
	g.Origin = origin
	g.Width = width

	g.Length = width[0]
	g.Area = width[0] * width[1]
	g.Volume = width[0] * width[1] * width[2]

	for i := 0; i < 3; i++ {
		g.uBounds[i] = g.Origin[i] + g.Width[i]
	}
}

func NewGridLocation(
	origin, width [3]int, boxWidth float64, cells int,
) *GridLocation {
	g := &GridLocation{}
	g.Init(origin, width, boxWidth, cells)
	return g
}

func (g *GridLocation) Init(
	origin, width [3]int, boxWidth float64, cells int,
) {
	g.Grid.Init(origin, width)
	g.BoxWidth = boxWidth
	g.Cells = cells
}

// Idx returns the grid index corresponding to a set of coordinates.
func (g *Grid) Idx(x, y, z int) int {
	// Those subtractions are actually unneccessary.
	return ((x - g.Origin[0]) + (y-g.Origin[1])*g.Length +
		(z-g.Origin[2])*g.Area)
}

// IdxCheck returns an index and true if the given coordinate are valid and
// false otherwise.
func (g *Grid) IdxCheck(x, y, z int) (idx int, ok bool) {
	if !g.BoundsCheck(x, y, z) {
		return -1, false
	}

	return g.Idx(x, y, z), true
}

// BoundsCheck returns true if the given coordinates are within the Grid and
// false otherwise.
func (g *Grid) BoundsCheck(x, y, z int) bool {
	return (g.Origin[0] <= x && g.Origin[1] <= y && g.Origin[2] <= z) &&
		(x < g.uBounds[0] && y < g.uBounds[1] &&
			z < g.uBounds[2])
}

// Coords returns the x, y, z coordinates of a point from its grid index.
func (g *Grid) Coords(idx int) (x, y, z int) {
	x = idx % g.Length
	y = (idx % g.Area) / g.Length
	z = idx / g.Area
	return x, y, z
}
/*
// pMod computes the positive modulo x % y.
func pMod(x, y int) int {
	m := x % y
	if m < 0 {
		m += y
	}
	return m
}
*/

// Intersect retursn true if the two bounding boxes overlap and false otherwise.
func (cb1 *CellBounds) Intersect(cb2 *CellBounds, width int) bool {
	intr := true
	var ( 
		oSmall, oBig, wSmall, wBig int
	)
	for i := 0; intr && i < 3; i++ {
		if cb1.Width[i] < cb2.Width[i] {
			oSmall, wSmall = cb1.Origin[i], cb1.Width[i]
			oBig, wBig = cb2.Origin[i], cb2.Width[i]
		} else {
			oSmall, wSmall = cb2.Origin[i], cb2.Width[i]
			oBig, wBig = cb1.Origin[i], cb1.Width[i]
		}

		eSmall := oSmall + wSmall
		beSmall := bound(eSmall, oBig, width)
		boSmall := bound(oSmall, oBig, width)

		intr = intr && (beSmall < wBig || boSmall < wBig)
	}
	return intr
}


func (cb1 *CellBounds) IntersectUnbounded(cb2 *CellBounds) bool {
	intr := true
	var (
		oLow, oHigh, wLow int
	)
	for i := 0; intr && i < 3; i++ {
		if cb1.Origin[i] < cb2.Origin[i] {
			oLow, oHigh, wLow = cb1.Origin[i], cb2.Origin[i], cb1.Width[i]
		} else {
			oLow, oHigh, wLow = cb2.Origin[i], cb1.Origin[i], cb2.Width[i]
		}

		intr = intr && (oLow + wLow > oHigh)
	}
	return intr
}

func bound(x, origin, width int) int {
	diff := x - origin
	if diff < 0 { return diff + width }
	if diff > width { return diff - width }
	return diff
}

func (vcb *CellBounds) ScaleVecsSegment(
	vs []Vec, cells int, boxWidth float64,
) {
	fCells := float32(cells)
	fWidth := float32(boxWidth)
	scale := fCells / fWidth

	origin := Vec{
		float32(vcb.Origin[0]),
		float32(vcb.Origin[1]),
		float32(vcb.Origin[2]),
	}

	for i := range vs {
		for j := 0; j < 3; j++ { 
			vs[i][j] *= scale
			vs[i][j] = vs[i][j] - origin[j]
			if vs[i][j] < 0 { vs[i][j] += fCells }
		}
	}
}

func (vcb *CellBounds) ScaleVecsDomain(
	cb *CellBounds, vs []Vec, cells int, boxWidth float64,
) {
	fCells := float32(cells)
	fWidth := float32(boxWidth)
	scale := fCells / fWidth

	origin := Vec{
		float32(vcb.Origin[0]),
		float32(vcb.Origin[1]),
		float32(vcb.Origin[2]),
	}
	diff := Vec{
		float32(vcb.Origin[0] - cb.Origin[0]),
		float32(vcb.Origin[1] - cb.Origin[1]),
		float32(vcb.Origin[2] - cb.Origin[2]),
	}

	for i := 0; i < 3; i++ {
		if diff[i] < -fCells/2 {
			diff[i] += fCells
		} else if diff[i] > fCells/2 {
			diff[i] -= fCells
		}
	}

	for i := range vs {
		for j := 0; j < 3; j++ { 
			vs[i][j] *= scale
			vs[i][j] = vs[i][j] - origin[j]
			if vs[i][j] < 0 { vs[i][j] += fCells }
			vs[i][j] += diff[j]
		}
	}
}

func maxV(vs []Vec, dim int) float32 {
	max := vs[0][dim]
	for i := range vs {
		if max < vs[i][dim] { max = vs[i][dim] }
	}
	return max
}

func minV(vs []Vec, dim int) float32 {
	min := vs[0][dim]
	for i := range vs {
		if min > vs[i][dim] { min = vs[i][dim] }
	}
	return min
}

func countInBounds(cb *CellBounds, vs []Vec) int {
	num := 0
	for _, v := range vs {
		if int(v[0]) < cb.Width[0] && v[0] > 0 &&
			int(v[1]) < cb.Width[1] && v[1] > 0 && 
			int(v[2]) < cb.Width[2] && v[2] > 0 {
			num++
		}
	}
	return num
}

func fMinMax(min, max, x float32) (float32, float32) {
	if x < min {
		return x, max
	}
	if x > max {
		return min, x
	}
	return min, max
}
