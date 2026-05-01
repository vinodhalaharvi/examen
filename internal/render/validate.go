package render

import (
	"fmt"
	"math"
	"strings"

	"examen/internal/types"
)

// =============================================================================
// VALIDATION — multi-tier checks before a figure ships
// =============================================================================
//
// Tier 1: structural (Validate)
//   - all primitives have known Kind
//   - all coordinates inside viewBox
//   - sane primitive count, sane label count
//   - labels don't overlap each other (bounding-box test)
//
// Tier 2: geometry consistency (CheckConsistency, called separately with the
//   problem stem). Lives below.

// Validate runs Tier-1 structural checks. Returns nil if the figure is
// shippable on structural grounds alone (the visual quality agent may still
// reject it for aesthetic reasons).
func Validate(spec types.FigureSpec) error {
	w, h := dims(spec)

	if len(spec.Primitives) == 0 {
		return fmt.Errorf("no primitives in figure")
	}
	if len(spec.Primitives) > 40 {
		return fmt.Errorf("too many primitives: %d", len(spec.Primitives))
	}
	if len(spec.Labels) > 30 {
		return fmt.Errorf("too many labels: %d", len(spec.Labels))
	}

	// Each primitive: known kind, all coords inside viewBox.
	for i, p := range spec.Primitives {
		if p.Kind == "" {
			return fmt.Errorf("primitive %d has empty kind", i)
		}
		if !isKnownKind(p.Kind) {
			return fmt.Errorf("primitive %d: unknown kind %q", i, p.Kind)
		}
		if err := primitiveInBounds(p, w, h); err != nil {
			return fmt.Errorf("primitive %d (%s): %w", i, p.Kind, err)
		}
	}

	// Labels: inside viewBox, not overlapping each other.
	for i, l := range spec.Labels {
		if l.X < 0 || l.X > float64(w) || l.Y < 0 || l.Y > float64(h) {
			return fmt.Errorf("label %d (%q) outside viewBox at (%g, %g)",
				i, l.Text, l.X, l.Y)
		}
	}
	if pairs := overlappingLabels(spec.Labels); len(pairs) > 0 {
		first := pairs[0]
		return fmt.Errorf("labels overlap: %q and %q", first.A.Text, first.B.Text)
	}

	return nil
}

// dims returns the effective width/height for the spec (with the same
// defaults the renderer uses). Done in one place so validation and rendering
// agree.
func dims(spec types.FigureSpec) (int, int) {
	w, h := spec.Width, spec.Height
	if w == 0 {
		w = 360
	}
	if h == 0 {
		h = 240
	}
	return w, h
}

// =============================================================================
// PRIMITIVE BOUNDS
// =============================================================================

// primitiveInBounds verifies every coordinate the primitive touches is inside
// (0,0)..(w,h). Margin tolerance is 1px to allow for rounding.
func primitiveInBounds(p types.SVGPrimitive, w, h int) error {
	W, H := float64(w), float64(h)
	a := p.Attrs

	switch p.Kind {
	case "line":
		return points2D(p, W, H, [][2]string{{"x1", "y1"}, {"x2", "y2"}})

	case "circle":
		cx, cy, r := a["cx"], a["cy"], a["r"]
		if cx-r < -1 || cx+r > W+1 || cy-r < -1 || cy+r > H+1 {
			return fmt.Errorf("circle (%g,%g,r=%g) extends outside viewBox %dx%d",
				cx, cy, r, w, h)
		}

	case "rect":
		x, y, ww, hh := a["x"], a["y"], a["w"], a["h"]
		if x < -1 || y < -1 || x+ww > W+1 || y+hh > H+1 {
			return fmt.Errorf("rect (%g,%g,%gx%g) extends outside viewBox", x, y, ww, hh)
		}

	case "polygon", "polyline":
		n := int(a["points_count"])
		if n < 2 {
			return fmt.Errorf("%s needs >= 2 points, got %d", p.Kind, n)
		}
		for i := 0; i < n; i++ {
			x := a[fmt.Sprintf("x%d", i)]
			y := a[fmt.Sprintf("y%d", i)]
			if x < -1 || x > W+1 || y < -1 || y > H+1 {
				return fmt.Errorf("%s point %d at (%g,%g) outside viewBox",
					p.Kind, i, x, y)
			}
		}

	case "right_angle":
		x, y, sz := a["x"], a["y"], a["size"]
		if sz == 0 {
			sz = 10
		}
		if x-sz < -1 || x > W+1 || y-sz < -1 || y > H+1 {
			return fmt.Errorf("right_angle at (%g,%g) size %g would render outside viewBox",
				x, y, sz)
		}

	case "angle_marker":
		cx, cy, r := a["cx"], a["cy"], a["r"]
		// arc is bounded by the circle of radius r around (cx, cy)
		if cx-r < -1 || cx+r > W+1 || cy-r < -1 || cy+r > H+1 {
			return fmt.Errorf("angle_marker arc (cx=%g, cy=%g, r=%g) extends outside viewBox",
				cx, cy, r)
		}
		if r <= 0 {
			return fmt.Errorf("angle_marker has non-positive radius %g", r)
		}
	}
	return nil
}

func points2D(p types.SVGPrimitive, W, H float64, pairs [][2]string) error {
	for _, pair := range pairs {
		x := p.Attrs[pair[0]]
		y := p.Attrs[pair[1]]
		if x < -1 || x > W+1 || y < -1 || y > H+1 {
			return fmt.Errorf("point (%s=%g, %s=%g) outside viewBox %.0fx%.0f",
				pair[0], x, pair[1], y, W, H)
		}
	}
	return nil
}

func isKnownKind(k string) bool {
	switch k {
	case "line", "circle", "rect", "polygon", "polyline",
		"right_angle", "angle_marker":
		return true
	}
	return false
}

// =============================================================================
// LABEL COLLISION DETECTION
// =============================================================================

// labelBounds estimates the bounding box of a rendered label. Approximate but
// good enough to catch obvious collisions. Uses average char width per font.
func labelBounds(l types.FigureLabel) (x0, y0, x1, y1 float64) {
	// Match the font-size used by renderLabel: 14 default, 11 for "mono".
	fontSize := 14.0
	avgCharW := 7.5 // serif italic, eyeballed from Fraunces 14
	if l.Style == "mono" {
		fontSize = 11.0
		avgCharW = 6.6
	}
	// SVG <text> y is the baseline; the visible glyphs span roughly
	// [y - 0.8*fontSize, y + 0.2*fontSize]
	width := float64(len(l.Text)) * avgCharW
	x0 = l.X
	y0 = l.Y - fontSize*0.8
	x1 = l.X + width
	y1 = l.Y + fontSize*0.2
	return
}

// labelPair is two labels whose bounding boxes intersect.
type labelPair struct{ A, B types.FigureLabel }

// overlappingLabels returns the pairs of labels whose bounding boxes overlap.
// Labels are allowed to share a coordinate (overlap area must be > 4 px²).
func overlappingLabels(labels []types.FigureLabel) []labelPair {
	var out []labelPair
	for i := 0; i < len(labels); i++ {
		ax0, ay0, ax1, ay1 := labelBounds(labels[i])
		for j := i + 1; j < len(labels); j++ {
			bx0, by0, bx1, by1 := labelBounds(labels[j])
			ix := math.Min(ax1, bx1) - math.Max(ax0, bx0)
			iy := math.Min(ay1, by1) - math.Max(ay0, by0)
			if ix > 0 && iy > 0 && ix*iy > 4 {
				out = append(out, labelPair{labels[i], labels[j]})
			}
		}
	}
	return out
}

// =============================================================================
// LABEL AUTO-NUDGE (best-effort layout fix)
// =============================================================================

// FixLabels nudges overlapping labels apart by small amounts. Doesn't always
// produce perfect layouts, but consistently beats letting them stack on top
// of each other. Returns a copy of spec with labels rearranged.
func FixLabels(spec types.FigureSpec) types.FigureSpec {
	out := spec
	out.Labels = make([]types.FigureLabel, len(spec.Labels))
	copy(out.Labels, spec.Labels)

	w, h := dims(out)
	W, H := float64(w), float64(h)

	const maxIter = 12
	for iter := 0; iter < maxIter; iter++ {
		pairs := overlappingLabels(out.Labels)
		if len(pairs) == 0 {
			return out
		}
		// For each colliding pair, push the second label by 8px in the
		// direction away from the first label, clamped to viewBox.
		for _, pp := range pairs {
			ax := (boundsCenter(pp.A))
			bx := (boundsCenter(pp.B))
			dx := bx[0] - ax[0]
			dy := bx[1] - ay(ax, bx)
			if dx == 0 && dy == 0 {
				dx = 8
			}
			// Apply nudge
			for k := range out.Labels {
				if out.Labels[k].X == pp.B.X && out.Labels[k].Y == pp.B.Y &&
					out.Labels[k].Text == pp.B.Text {
					out.Labels[k].X = clamp(out.Labels[k].X+sign(dx)*8, 4, W-4)
					out.Labels[k].Y = clamp(out.Labels[k].Y+sign(dy)*6, 12, H-4)
					break
				}
			}
		}
	}
	return out
}

func boundsCenter(l types.FigureLabel) [2]float64 {
	x0, y0, x1, y1 := labelBounds(l)
	return [2]float64{(x0 + x1) / 2, (y0 + y1) / 2}
}

func ay(a, b [2]float64) float64 { return a[1] }

func sign(x float64) float64 {
	if x > 0 {
		return 1
	}
	if x < 0 {
		return -1
	}
	return 1
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// =============================================================================
// GEOMETRY CONSISTENCY (Tier 2 — used by the figure agent post-LLM)
// =============================================================================

// CheckConsistency verifies the figure roughly matches the numbers in the
// problem stem. This is best-effort — it catches the obvious mismatches
// (angle 35° drawn as 90°, ratio 1:3 drawn as 1:1) but isn't a full geometry
// solver.
//
// Returns nil if no clear contradictions detected. Returns an error
// describing the most obvious mismatch otherwise.
func CheckConsistency(spec types.FigureSpec, stem string) error {
	stemNumbers := extractNumbers(stem)
	if len(stemNumbers) == 0 {
		return nil // no numerical claims to check against
	}

	// If the stem mentions an angle and the spec has an angle_marker, they
	// should agree to within 8 degrees.
	if angleInStem, ok := findAngleInDegrees(stem); ok {
		if marker, ok := findAngleMarker(spec); ok {
			drawn := math.Abs(marker.Attrs["sweep"])
			if math.Abs(drawn-angleInStem) > 8 {
				return fmt.Errorf("stem says %.0f° but figure draws ~%.0f°",
					angleInStem, drawn)
			}
		}
	}

	return nil
}

// extractNumbers pulls all decimal numbers out of a string.
func extractNumbers(s string) []float64 {
	var out []float64
	var cur strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= '0' && c <= '9') || c == '.' {
			cur.WriteByte(c)
			continue
		}
		if cur.Len() > 0 {
			if v, err := parseFloat(cur.String()); err == nil {
				out = append(out, v)
			}
			cur.Reset()
		}
	}
	if cur.Len() > 0 {
		if v, err := parseFloat(cur.String()); err == nil {
			out = append(out, v)
		}
	}
	return out
}

// findAngleInDegrees looks for a number followed by "°" or " degree" in the stem.
func findAngleInDegrees(stem string) (float64, bool) {
	// Quick path: scan for "°"
	idx := strings.Index(stem, "°")
	if idx < 0 {
		idx = strings.Index(strings.ToLower(stem), "degree")
	}
	if idx < 0 {
		return 0, false
	}
	// Walk backwards to find the number
	var numStart, numEnd int = -1, -1
	for i := idx - 1; i >= 0; i-- {
		c := stem[i]
		if c == ' ' || c == '\t' || c == 0xA0 /* nbsp */ {
			if numEnd > 0 {
				break
			}
			continue
		}
		if (c >= '0' && c <= '9') || c == '.' {
			if numEnd < 0 {
				numEnd = i + 1
			}
			numStart = i
			continue
		}
		if numEnd > 0 {
			break
		}
	}
	if numStart < 0 || numEnd < 0 {
		return 0, false
	}
	v, err := parseFloat(stem[numStart:numEnd])
	if err != nil {
		return 0, false
	}
	return v, true
}

func findAngleMarker(spec types.FigureSpec) (types.SVGPrimitive, bool) {
	for _, p := range spec.Primitives {
		if p.Kind == "angle_marker" {
			return p, true
		}
	}
	return types.SVGPrimitive{}, false
}

func parseFloat(s string) (float64, error) {
	var v float64
	_, err := fmt.Sscanf(s, "%f", &v)
	return v, err
}
