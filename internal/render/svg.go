// Package render converts FigureSpec values into SVG markup.
// The renderer is the trust boundary between LLM output and what the user sees:
// invalid specs are caught here, not in the browser.
package render

import (
	"fmt"
	"math"
	"strings"

	"examen/internal/types"
)

// SVG renders a FigureSpec to a complete <svg>...</svg> string.
func SVG(spec types.FigureSpec) string {
	var sb strings.Builder
	w, h := spec.Width, spec.Height
	if w == 0 {
		w = 360
	}
	if h == 0 {
		h = 240
	}
	vb := spec.ViewBox
	if vb == "" {
		vb = fmt.Sprintf("0 0 %d %d", w, h)
	}
	fmt.Fprintf(&sb, `<svg viewBox="%s" xmlns="http://www.w3.org/2000/svg" width="100%%" height="auto">`, vb)
	for _, p := range spec.Primitives {
		sb.WriteString(renderPrimitive(p))
	}
	for _, l := range spec.Labels {
		sb.WriteString(renderLabel(l))
	}
	sb.WriteString(`</svg>`)
	return sb.String()
}

func styleAttrs(style string) string {
	switch style {
	case "highlight":
		return `stroke="#c2410c" stroke-width="3" fill="none"`
	case "dashed":
		return `stroke="#78716c" stroke-width="1" stroke-dasharray="3 3" fill="none"`
	case "soft_fill":
		return `fill="#fef3c7" stroke="#c2410c" stroke-width="1.5"`
	default:
		return `stroke="#1a1612" stroke-width="2" fill="none"`
	}
}

func renderPrimitive(p types.SVGPrimitive) string {
	a := p.Attrs
	idAttr := ""
	if p.ID != "" {
		idAttr = fmt.Sprintf(` id="%s"`, p.ID)
	}
	switch p.Kind {
	case "line":
		return fmt.Sprintf(`<line x1="%g" y1="%g" x2="%g" y2="%g" %s%s/>`,
			a["x1"], a["y1"], a["x2"], a["y2"], styleAttrs(p.Style), idAttr)
	case "circle":
		return fmt.Sprintf(`<circle cx="%g" cy="%g" r="%g" %s%s/>`,
			a["cx"], a["cy"], a["r"], styleAttrs(p.Style), idAttr)
	case "rect":
		return fmt.Sprintf(`<rect x="%g" y="%g" width="%g" height="%g" %s%s/>`,
			a["x"], a["y"], a["w"], a["h"], styleAttrs(p.Style), idAttr)
	case "polygon":
		// expects points_count + x0,y0,x1,y1,...
		n := int(a["points_count"])
		var pts []string
		for i := 0; i < n; i++ {
			pts = append(pts, fmt.Sprintf("%g,%g", a[fmt.Sprintf("x%d", i)], a[fmt.Sprintf("y%d", i)]))
		}
		return fmt.Sprintf(`<polygon points="%s" %s%s/>`,
			strings.Join(pts, " "), styleAttrs(p.Style), idAttr)
	case "right_angle":
		// Small L-shape at (x, y) with given size; opens up-left by default
		x, y, sz := a["x"], a["y"], a["size"]
		if sz == 0 {
			sz = 10
		}
		return fmt.Sprintf(
			`<polyline points="%g,%g %g,%g %g,%g" stroke="#1a1612" stroke-width="1.5" fill="none"/>`,
			x-sz, y, x-sz, y-sz, x, y-sz)
	case "angle_marker":
		// Arc at center (cx, cy) radius r from start° to start+sweep°
		cx, cy, r := a["cx"], a["cy"], a["r"]
		startDeg, sweepDeg := a["start"], a["sweep"]
		x1 := cx + r*math.Cos(deg2rad(startDeg))
		y1 := cy - r*math.Sin(deg2rad(startDeg))
		x2 := cx + r*math.Cos(deg2rad(startDeg+sweepDeg))
		y2 := cy - r*math.Sin(deg2rad(startDeg+sweepDeg))
		largeArc := 0
		if math.Abs(sweepDeg) > 180 {
			largeArc = 1
		}
		sweepFlag := 0
		if sweepDeg > 0 {
			sweepFlag = 0
		} else {
			sweepFlag = 1
		}
		return fmt.Sprintf(
			`<path d="M %g %g A %g %g 0 %d %d %g %g" stroke="#c2410c" stroke-width="2" fill="none"/>`,
			x1, y1, r, r, largeArc, sweepFlag, x2, y2)
	case "polyline":
		// expects points_count + x0,y0,x1,y1,...
		n := int(a["points_count"])
		var pts []string
		for i := 0; i < n; i++ {
			pts = append(pts, fmt.Sprintf("%g,%g", a[fmt.Sprintf("x%d", i)], a[fmt.Sprintf("y%d", i)]))
		}
		return fmt.Sprintf(`<polyline points="%s" %s%s/>`,
			strings.Join(pts, " "), styleAttrs(p.Style), idAttr)
	}
	return ""
}

func renderLabel(l types.FigureLabel) string {
	style := `font-family="Fraunces, serif" font-size="14" font-style="italic" fill="#1a1612"`
	switch l.Style {
	case "highlight":
		style = `font-family="Fraunces, serif" font-size="14" font-style="italic" fill="#c2410c"`
	case "mono":
		style = `font-family="JetBrains Mono, monospace" font-size="11" fill="#78716c"`
	}
	return fmt.Sprintf(`<text x="%g" y="%g" %s>%s</text>`,
		l.X, l.Y, style, escapeXML(l.Text))
}

func deg2rad(d float64) float64 { return d * math.Pi / 180 }

func escapeXML(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
	)
	return r.Replace(s)
}

// Validate checks that a FigureSpec is well-formed and primitives stay within
// the viewbox. Returns nil if valid.
func Validate(spec types.FigureSpec) error {
	if spec.Width <= 0 || spec.Height <= 0 {
		return fmt.Errorf("invalid dimensions: %dx%d", spec.Width, spec.Height)
	}
	if len(spec.Primitives) == 0 {
		return fmt.Errorf("no primitives in figure")
	}
	for i, p := range spec.Primitives {
		if p.Kind == "" {
			return fmt.Errorf("primitive %d has empty kind", i)
		}
	}
	return nil
}
