package render

import (
	"strings"
	"testing"

	"examen/internal/types"
)

func validSpec() types.FigureSpec {
	return types.FigureSpec{
		Width:   360,
		Height:  240,
		ViewBox: "0 0 360 240",
		Primitives: []types.SVGPrimitive{
			{
				Kind:  "line",
				Attrs: map[string]float64{"x1": 20, "y1": 200, "x2": 340, "y2": 200},
				Style: "default",
			},
			{
				Kind:  "circle",
				Attrs: map[string]float64{"cx": 180, "cy": 100, "r": 40},
				Style: "default",
			},
		},
		Labels: []types.FigureLabel{
			{Text: "A", X: 30, Y: 30},
			{Text: "B", X: 200, Y: 30},
		},
	}
}

func TestValidate_Happy(t *testing.T) {
	if err := Validate(validSpec()); err != nil {
		t.Fatalf("valid spec rejected: %v", err)
	}
}

func TestValidate_RejectsEmptyPrimitives(t *testing.T) {
	s := validSpec()
	s.Primitives = nil
	if err := Validate(s); err == nil {
		t.Fatal("expected rejection for empty primitives")
	}
}

func TestValidate_RejectsUnknownKind(t *testing.T) {
	s := validSpec()
	s.Primitives = append(s.Primitives, types.SVGPrimitive{
		Kind:  "octahedron",
		Attrs: map[string]float64{},
	})
	err := Validate(s)
	if err == nil || !strings.Contains(err.Error(), "unknown kind") {
		t.Fatalf("expected unknown-kind rejection, got %v", err)
	}
}

func TestValidate_RejectsLineOutsideViewBox(t *testing.T) {
	s := validSpec()
	s.Primitives[0].Attrs["x2"] = 500 // viewBox is 360 wide
	if err := Validate(s); err == nil {
		t.Fatal("expected rejection for line extending past viewBox")
	}
}

func TestValidate_RejectsCircleProtruding(t *testing.T) {
	s := validSpec()
	// Circle with center near right edge and large radius — protrudes
	s.Primitives[1] = types.SVGPrimitive{
		Kind:  "circle",
		Attrs: map[string]float64{"cx": 350, "cy": 100, "r": 40},
	}
	err := Validate(s)
	if err == nil || !strings.Contains(err.Error(), "circle") {
		t.Fatalf("expected circle-OOB rejection, got %v", err)
	}
}

func TestValidate_RejectsPolygonPointOutside(t *testing.T) {
	s := validSpec()
	s.Primitives = append(s.Primitives, types.SVGPrimitive{
		Kind: "polygon",
		Attrs: map[string]float64{
			"points_count": 3,
			"x0": 10, "y0": 10,
			"x1": 100, "y1": 10,
			"x2": 50, "y2": 999, // out of bounds
		},
	})
	if err := Validate(s); err == nil {
		t.Fatal("expected polygon point rejection")
	}
}

func TestValidate_RejectsLabelOutsideViewBox(t *testing.T) {
	s := validSpec()
	s.Labels = []types.FigureLabel{{Text: "X", X: -10, Y: 50}}
	if err := Validate(s); err == nil {
		t.Fatal("expected label-OOB rejection")
	}
}

func TestValidate_RejectsOverlappingLabels(t *testing.T) {
	s := validSpec()
	// Two labels stacked on top of each other
	s.Labels = []types.FigureLabel{
		{Text: "ABCD", X: 100, Y: 100},
		{Text: "WXYZ", X: 100, Y: 100},
	}
	err := Validate(s)
	if err == nil || !strings.Contains(err.Error(), "overlap") {
		t.Fatalf("expected overlap rejection, got %v", err)
	}
}

func TestValidate_AllowsLabelsWellSeparated(t *testing.T) {
	s := validSpec()
	s.Labels = []types.FigureLabel{
		{Text: "A", X: 30, Y: 30},
		{Text: "B", X: 200, Y: 30},  // same y, but 170px apart horizontally
		{Text: "C", X: 30, Y: 100},  // same x, but 70px down
	}
	if err := Validate(s); err != nil {
		t.Fatalf("expected no overlap for well-spaced labels, got %v", err)
	}
}

func TestFixLabels_ResolvesSimpleCollision(t *testing.T) {
	s := validSpec()
	s.Labels = []types.FigureLabel{
		{Text: "AAA", X: 100, Y: 100},
		{Text: "BBB", X: 100, Y: 100}, // exact same spot
	}
	fixed := FixLabels(s)
	if pairs := overlappingLabels(fixed.Labels); len(pairs) > 0 {
		t.Errorf("FixLabels did not resolve overlap; still %d pairs colliding", len(pairs))
	}
}

func TestExtractNumbers(t *testing.T) {
	cases := []struct {
		in   string
		want []float64
	}{
		{"shadow 24 m, angle 35°", []float64{24, 35}},
		{"radius 10.5 and circumference 65.97", []float64{10.5, 65.97}},
		{"no numbers here", nil},
	}
	for _, tc := range cases {
		got := extractNumbers(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("extractNumbers(%q): got %v, want %v", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("extractNumbers(%q)[%d]: got %v, want %v",
					tc.in, i, got[i], tc.want[i])
			}
		}
	}
}

func TestFindAngleInDegrees(t *testing.T) {
	cases := []struct {
		in       string
		want     float64
		wantOK   bool
	}{
		{"angle of 35°", 35, true},
		{"the 110° central angle", 110, true},
		{"a 45 degree angle", 45, true},
		{"no angle here", 0, false},
	}
	for _, tc := range cases {
		got, ok := findAngleInDegrees(tc.in)
		if ok != tc.wantOK || (ok && got != tc.want) {
			t.Errorf("findAngleInDegrees(%q): got (%v,%v), want (%v,%v)",
				tc.in, got, ok, tc.want, tc.wantOK)
		}
	}
}

func TestCheckConsistency_DetectsAngleMismatch(t *testing.T) {
	s := validSpec()
	s.Primitives = append(s.Primitives, types.SVGPrimitive{
		Kind:  "angle_marker",
		Attrs: map[string]float64{"cx": 100, "cy": 100, "r": 30, "start": 0, "sweep": 90},
	})
	if err := CheckConsistency(s, "angle of 35°"); err == nil {
		t.Fatal("expected detection of angle mismatch (90 drawn vs 35 stated)")
	}
	if err := CheckConsistency(s, "no angle here"); err != nil {
		t.Errorf("expected no error when stem has no angle: %v", err)
	}
}

func TestCheckConsistency_AllowsCloseMatch(t *testing.T) {
	s := validSpec()
	s.Primitives = append(s.Primitives, types.SVGPrimitive{
		Kind:  "angle_marker",
		Attrs: map[string]float64{"cx": 100, "cy": 100, "r": 30, "start": 0, "sweep": -33},
	})
	// 33° drawn vs 35° in stem — within 8° tolerance
	if err := CheckConsistency(s, "angle of 35°"); err != nil {
		t.Errorf("expected no error for close match, got %v", err)
	}
}
