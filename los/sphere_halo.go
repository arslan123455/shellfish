/*package sphere_halo is essentially a redo of the implementation of
HaloProfiles found in the los package but with a different internal geometry
kernel. I've learned a few lessons since then about the right way to structure
this stuff and I'm going to try applying those lessons here.

Operating on a SphereHalo is relatively simple:

    hs := make([]SphereHalo, workers)
    h := &hs[0]
    h.Init(norms, origin, rMin, rMax, bins, n)

    // Read particle positions from disk. (Probably in a loop.)
    vecs := Read()

    h.Transform(vecs)
    intr := make([]bool, len(vecs))
    h.Intersect(vecs, intr)

    // Split the halo up into thread-specific workplaces.
    h.Split(hs)

   // Split into multiple thread here

   for i, vec := range vecs {
       if intr[i] { h.Insert(vec, ptRadius) }
    }

    // Do synchronization here

    h.Join(hs)
*/
package los

import (
	"fmt"
	"math"

	"github.com/phil-mansfield/shellfish/render/io"
	"github.com/phil-mansfield/shellfish/math/mat"
	"github.com/phil-mansfield/shellfish/math/sort"
	"github.com/phil-mansfield/shellfish/los/geom"
)

// Type SphereHalo represents a halo which can have spheres inserted into it.
type SphereHalo struct {
	origin [3]float64
	rings, bins, n int // bins = radial bins, n = number of lines per 
	rMin, rMax float64
	rs []float64

	ringVecs [][2]float64
	ringPhis []float64
	dPhi float64

	rots, irots []mat.Matrix32
	norms [][3]float32
	profs []ProfileRing

	defaultRho float64
}

// Init initializes a halo centered at origin with minimum and maximum radii
// given by rMin, and rMax. It will consist of a family of rings whose normals
// are given by the slice of vectors, norms. Each ring will consists of n
// lines of sight and will have bins radial bins.
func (h *SphereHalo) Init(
	norms [][3]float32, origin [3]float64,
	rMin, rMax float64, bins, n int,
	defaultRho float64,
) {
	h.origin = origin
	h.rMin, h.rMax = rMin, rMax
	h.rs = make([]float64, h.bins)

	h.rings, h.bins, h.n = len(norms), bins, n
	h.norms = norms

	h.defaultRho = defaultRho

	zAxis := &[3]float32{0, 0, 1}

	h.profs = make([]ProfileRing, h.rings)
	h.rots = make([]mat.Matrix32, h.rings)
	h.irots = make([]mat.Matrix32, h.rings)

	for i := range h.profs {
		h.profs[i].Init(math.Log(h.rMin), math.Log(h.rMax), h.bins, h.n)
		h.rots[i].Init(make([]float32, 9), 3, 3)
		h.irots[i].Init(make([]float32, 9), 3, 3)
		geom.EulerMatrixBetweenAt(&norms[i], zAxis, &h.rots[i])
		geom.EulerMatrixBetweenAt(zAxis, &norms[i], &h.irots[i])
	}

	h.ringPhis = make([]float64, h.n)
	h.ringVecs = make([][2]float64, h.n)
	for i := 0; i < h.n; i++ {
		h.ringPhis[i] = float64(i) / float64(n) * (2 * math.Pi)
		h.ringVecs[i][1], h.ringVecs[i][0] = math.Sincos(h.ringPhis[i])
	}
	h.dPhi = 1 / float64(n) * (2 * math.Pi)
}

// Split splits the halo h into copies and stores those copies in hs. The
// total mass stored in h and all those copies is equal to the total mass
// stored in h.
//
// Used for parallelization. But very expensive.
func (h *SphereHalo) Split(hs []SphereHalo) {
	for i := range hs {
		hi := &hs[i]
		if h.rings != hi.rings || h.bins != hi.bins || h.n != hi.n {
			hi.Init(h.norms, h.origin, h.rMin,
				h.rMax, h.bins, h.n, h.defaultRho)
		} else {
			hi.norms = h.norms
			hi.rots = h.rots
			hi.origin = h.origin
			hi.rMin, hi.rMax = h.rMin, h.rMax
		}
		for r := range h.profs {
			h.profs[r].Split(&hi.profs[r])
		}
	}
}

// Join joins h and all the halos in hs together into h. The mass stored in h
// at the end is equal to the total mass intially in h and all the halos in hs.
//
// Used for parallelization. But very expensive.
func (h *SphereHalo) Join(hs []SphereHalo) {
	for i := range hs {
		hi := &hs[i]
		if h.rings != hi.rings || h.bins != hi.bins || h.n != hi.n {
			panic(fmt.Sprintf("size of h != size of hs[%d]", i))
		}

		for r := range h.profs {
			h.profs[r].Join(&hi.profs[r])
		}
	}
}

// Intersect treats all the given vectors as spheres of radius r, and tests
// them for intersection with the halo. The results are written to the
// buffer intr.
//
// Intersect must be called after Transform is called on the vectors.
func (h *SphereHalo) Intersect(vecs [][3]float32, r float64, intr []bool) {
	rMin, rMax := h.rMin - r, h.rMax + r
	if rMin < 0 { rMin = 0 }
	rMin2, rMax2 := float32(rMin*rMin), float32(rMax*rMax)
	
	if len(intr) != len(vecs) { panic("len(intr) != len(vecs)") }

	x0, y0, z0 := float32(h.origin[0]),float32(h.origin[1]),float32(h.origin[2])
	for i, vec := range vecs {
		x, y, z := vec[0]-x0, vec[1]-y0, vec[2]-z0
		r2 := x*x + y*y + z*z
		intr[i] = r2 > rMin2 && r2 < rMax2
	}
}

// Transform translates all the given vectors so that they are in the local
// coordinate system of the halo.
func (h *SphereHalo) Transform(vecs [][3]float32, totalWidth float64) {
	x0 := float32(h.origin[0])
	y0 := float32(h.origin[1])
	z0 := float32(h.origin[2])
	tw := float32(totalWidth)
	tw2 := tw / 2
	
	for i, vec := range vecs {
		x, y, z := vec[0], vec[1], vec[2]
		dx, dy, dz := x - x0, y - y0, z - z0
		
        if dx > tw2 {
            vecs[i][0] -= tw
        } else if dx < -tw2 {
            vecs[i][0] += tw
        }

        if dy > tw2 {
            vecs[i][1] -= tw
        } else if dy < -tw2 {
            vecs[i][1] += tw
        }

        if dz > tw2 {
            vecs[i][2] -= tw
        } else if dz < -tw2 {
            vecs[i][2] += tw
        }
	}
}

// Insert insreats a sphere with the given center and radius to all the rings
// of the halo.
func (h *SphereHalo) Insert(vec [3]float32, radius, rho float64) {
	// transform into displacement from the center
	vec[0] -= float32(h.origin[0])
	vec[1] -= float32(h.origin[1])
	vec[2] -= float32(h.origin[2])

	for ring := 0; ring < h.rings; ring++ {
		// If this intersection check is the chief cost, we can throw some
		// more computational feometry at it until it's fixed. (3D spatial
		// indexing trees.)
		if h.sphereIntersectRing(vec, radius, ring) {
			h.insertToRing(vec, radius, rho, ring)
		}
	}
}

// sphereIntersecRing performs an intersection
func (h *SphereHalo) sphereIntersectRing(
	vec [3]float32, radius float64, ring int,
) bool {
	norm := h.norms[ring]
	dot := float64(norm[0]*vec[0] + norm[1]*vec[1] + norm[2]*vec[2])
	return dot < radius && dot > -radius 
}

// insertToRing inserts a sphere of the given center, radius, and density to
// one ring of the halo. This is where the magic happens.
func (h *SphereHalo) insertToRing(
	vec [3]float32, radius, rho float64, ring int,
) {
	geom.RotateVec(&vec, &h.rots[ring])

	// Properties of the projected circle.
	cx, cy, cz := float64(vec[0]), float64(vec[1]), float64(vec[2])
	projDist2 := cx*cx + cy*cy
	projRad2 := radius*radius - cz*cz
	if projRad2 < 0 { projRad2 = 0 }
	
	if projRad2 > projDist2 {
		// Circle contains center.

		for i := 0; i < h.n; i++ {
			// b = impact parameter
			b := cy*h.ringVecs[i][0] - cx*h.ringVecs[i][1]
			dir := cx*h.ringVecs[i][0] + cy*h.ringVecs[i][1]
			rHi := oneValIntrDist(projDist2, projRad2, b, dir)
			h.profs[ring].Insert(math.Inf(-1), math.Log(rHi), rho, i)
		}
	} else {
		// Circle does not contain center.
		alpha := halfAngularWidth(projDist2, projRad2)
		projPhi := math.Atan2(cy, cx)
		phiStart, phiEnd := projPhi-alpha, projPhi+alpha
		iLo1, iHi1, iLo2, iHi2 := h.idxRange(phiStart, phiEnd)

		for i := iLo1; i < iHi1; i++ {
			// b = impact parameter			
			b := cy*h.ringVecs[i][0] - cx*h.ringVecs[i][1]
			rLo, rHi := twoValIntrDist(projDist2, projRad2, b)
			if math.IsNaN(rLo) || math.IsNaN(rHi) { continue }
			h.profs[ring].Insert(math.Log(rLo), math.Log(rHi), rho, i)
		}

		for i := iLo2; i < iHi2; i++ {
			b := cy*h.ringVecs[i][0] - cx*h.ringVecs[i][1]
			rLo, rHi := twoValIntrDist(projDist2, projRad2, b)
			if math.IsNaN(rLo) || math.IsNaN(rHi) { continue }
			h.profs[ring].Insert(math.Log(rLo), math.Log(rHi), rho, i)
		}
	}
}

// idxRange returns the range of indices spanned by the two given angles.
// Since it is possible that the indices map to non-contiguous potions of the
// LoS array, two sets of indices are returned and bot sets must be looped over.
//
// Upper indices are _exclusive_.
func (h *SphereHalo) idxRange(
	phiLo, phiHi float64,
) (iLo1, iHi1, iLo2, iHi2 int) {
	// An alternate approach involves doing some modulo calculations.
	// It is simpler, but slower.
	switch {
	case phiHi > 2*math.Pi:
		// phiHi wraps around.
		iLo1 = int(phiLo/h.dPhi)
		iHi1 = h.n
		iLo2 = 0
		iHi2 = int((phiHi - 2*math.Pi)/h.dPhi) + 1
		return iLo1, iHi1, iLo2, iHi2
	case phiLo < 0:
		// phiLo wraps around.
		iLo1 = int((phiLo + 2*math.Pi)/h.dPhi)
		iHi1 = h.n
		iLo2 = 0
		iHi2 = int(phiHi/h.dPhi) + 1
		return iLo1, iHi1, iLo2, iHi2
	default:
		// not wrapping around at all.
		iLo := int(phiLo/h.dPhi)
		iHi := int(phiHi/h.dPhi)+  1
		return iLo, iHi, 0, 0
	}
}

// angularWidth returns the angular width in radians of a circle of at a
// squared distance of dist2 and a squared radius of r2. It's assumed that
// the circle does not contain the origin.
func halfAngularWidth(dist2, r2 float64) float64 {
	return math.Asin(math.Sqrt(r2/dist2))
}


// twoValIntrDist returns both the intersection distances for a ray which
// passes through a circle at two points. dist2 is the squared distance
// between the origin of the ray and the center of the circle, rad2 is the
// squared radius of the circle, and b is the impact parameter of the
// ray and the center of the circle.
func twoValIntrDist(dist2, rad2, b float64) (lo, hi float64) {
	b2 := b*b
	midDist := math.Sqrt(dist2 - b2)
	diff := math.Sqrt(rad2 - b2)
	return midDist-diff, midDist+diff
}

// twoValIntrDist returns both the intersection distances for a ray which
// passes through a circle at one point. dist2 is the squared distance
// between the origin of the ray and the center of the circle, rad2 is the
// squared radius of the circle, b is the impact parameter of the
// ray and the center of the circle, and dir is the dot product of the
// the circle's position vector and the normal vector of .
func oneValIntrDist(dist2, rad2, b, dir float64) float64 {
	b2 := b*b
	radMidDist := math.Sqrt(rad2 - b2)
	cMidDist := math.Sqrt(dist2 - b2)
	if dir > 0 {
		return radMidDist + cMidDist
	} else {
		return radMidDist - cMidDist
	}
}

// GetRhos writes the density of the LoS at a given ring and LoS profile
// into buf.
func (h *SphereHalo) GetRhos(ring, losIdx int, buf []float64) {
	h.profs[ring].Retrieve(losIdx, buf)
	for i, rho := range buf {
		if rho < h.defaultRho { buf[i] = h.defaultRho }
	}
}

// GetRs writes the radial values of each bin into a a buffer.
func (h *SphereHalo) GetRs(buf []float64) {
	if len(buf) != h.bins { panic("|buf| != h.bins") }

	dlr := (math.Log(h.rMax) - math.Log(h.rMin))/float64(h.bins)
	lrMin := math.Log(h.rMin)
	for i := range buf { buf[i] = math.Exp(lrMin + dlr*(float64(i)+0.5)) }
}

// MedianProfile computes the median value of all the halo's LoS profiles.
func (h *SphereHalo) MedianProfile() []float64 {
	// Read Densities
	rhoBufs := make([][]float64, h.n * h.rings)
	for i := range rhoBufs {
		rhoBufs[i] = make([]float64, h.bins)
	}
	
	idx := 0
	for r := 0; r < h.rings; r++ {
		for prof := 0; prof < h.n; prof++ {
			h.GetRhos(r, prof, rhoBufs[idx])
			idx++
		}
	}
	
	// Find median of each radial bin.
	medBuf := make([]float64, h.n * h.rings)
	out := make([]float64, h.bins)
	for j := 0; j < h.bins; j++ {
		for i := range medBuf {
			medBuf[i] = rhoBufs[i][j]
		}
		out[j] = sort.Median(medBuf, medBuf)
	}
	
	return out
}

// MeanProfile computes the mean value of all the halo's LoS profiles.
func (h *SphereHalo) MeanProfile() []float64 {
	mean := make([]float64, h.bins)
	buf := make([]float64, h.bins)
	
	// Find the spherically averaged rho profile
	for r := 0; r < h.rings; r++ {
		for i := 0; i < h.n; i++ {
			h.GetRhos(r, i, buf)
			for j := range buf { mean[j] += buf[j] }
		}
	}

	n := float64(h.rings * h.n)
	for j := range mean { mean[j] /= n }
	return mean
}

// Phi returns the angle corresponding to the given LoS index.
func (h *SphereHalo) Phi(losIdx int) float64 { return h.ringPhis[losIdx] }

// LineSegment calculates a line segment corresponding to a given profile
// and writes it to out.
func (h *SphereHalo) LineSegment(ring, losIdx int, out *geom.LineSegment) {
	vec :=[3]float32{}
    sin, cos := math.Sincos(float64(h.ringPhis[losIdx]))
    vec[0], vec[1] = float32(cos), float32(sin)
    geom.RotateVec(&vec, &h.irots[ring])

	origin := [3]float32{
		float32(h.origin[0]), float32(h.origin[1]), float32(h.origin[2]),
	}

	*out = geom.LineSegment{ Origin: origin, Dir: vec,
		StartR: float32(h.rMin), EndR: float32(h.rMax) }
}

func wrapDist(x1, x2, width float64) float64 {
	dist := x1 - x2
	if dist > width / 2 {
		return dist - width
	} else if dist < width / -2 {
		return dist + width
	} else {
		return dist
	}
}

func inRange(x, r, low, width, tw float64) bool {
	return wrapDist(x, low, tw) > -r && wrapDist(x, low + width, tw) < r
}

// SheetIntersect returns true if the given halo and sheet intersect one another
// and false otherwise.
func (h *SphereHalo) SheetIntersect(hd *io.SheetHeader) bool {
	return inRange(h.origin[0], h.rMax, float64(hd.Origin[0]),
		float64(hd.Width[0]), hd.TotalWidth) &&
			inRange(h.origin[1], h.rMax, float64(hd.Origin[1]),
			float64(hd.Width[1]), hd.TotalWidth) &&
				inRange(h.origin[2], h.rMax, float64(hd.Origin[2]), 
				float64(hd.Width[2]), hd.TotalWidth)
}

// PlaneToVolume converts an (x, y) coordinate in the coordinate system of
// a given ring into the box's coordinate system.
func (h *SphereHalo) PlaneToVolume(ring int, px, py float64) (x, y, z float64) {
	v := &[3]float32{float32(px), float32(py), 0}
	geom.RotateVec(v, &h.irots[ring])
	return float64(v[0]), float64(v[1]), float64(v[2])
}

// RMax returns maximum radius of the halo profiles.
func (h *SphereHalo) RMax() float64 { return h.rMax }
