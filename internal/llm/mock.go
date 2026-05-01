package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"time"
)

// NewMock returns a Client that produces deterministic outputs for geometry
// and algebra problem generation, figure rendering, and validation.
//
// The mock simulates realistic latency (100-400ms per call) so the agent
// trace UI shows the parallel/sequential structure clearly.
func NewMock() *Client {
	rng := rand.New(rand.NewSource(42))
	bank := newMockBank()

	return &Client{
		Name: "mock",
		Complete: func(ctx context.Context, req Request) (Response, error) {
			// Simulate latency
			latency := time.Duration(80+rng.Intn(220)) * time.Millisecond
			select {
			case <-ctx.Done():
				return Response{}, ctx.Err()
			case <-time.After(latency):
			}

			// Route by what the prompt is asking for.
			out := bank.respond(req, rng)
			return Response{
				Text:       out,
				StopReason: "end_turn",
				TokensIn:   len(req.User) / 4,
				TokensOut:  len(out) / 4,
			}, nil
		},
	}
}

// mockBank holds deterministic seed responses by intent + topic.
type mockBank struct {
	geometryProblems []geomTemplate
	algebraProblems  []algTemplate
}

type geomTemplate struct {
	id         string
	skill      string
	stemFmt    string
	answer     string
	numeric    float64
	tolerance  float64
	steps      []string
	explanation string
	figureSpec  string // JSON
	choices     string // JSON array
}

type algTemplate struct {
	id         string
	skill      string
	stem       string
	answer     string
	numeric    float64
	tolerance  float64
	steps      []string
	explanation string
	choices    string
}

func newMockBank() *mockBank {
	return &mockBank{
		geometryProblems: geomTemplates(),
		algebraProblems:  algTemplates(),
	}
}

func (b *mockBank) respond(req Request, rng *rand.Rand) string {
	prompt := req.User + " " + req.System
	lower := strings.ToLower(prompt)

	switch {
	case strings.Contains(lower, "generate problem") || strings.Contains(lower, "generate a math problem"):
		return b.problemResponse(prompt, rng)
	case strings.Contains(lower, "generate figure") || strings.Contains(lower, "figurespec") || strings.Contains(lower, "figure for problem"):
		return b.figureResponse(prompt, rng)
	case strings.Contains(lower, "generate distractors") || strings.Contains(lower, "wrong answers") ||
		strings.Contains(lower, "generate 4 choices") || strings.Contains(lower, "multiple-choice"):
		return b.distractorResponse(prompt, rng)
	case strings.Contains(lower, "validate") || strings.Contains(lower, "verify"):
		return b.validationResponse(prompt)
	case strings.Contains(lower, "coach") || strings.Contains(lower, "weakness") || strings.Contains(lower, "recent scores"):
		return b.coachResponse(prompt)
	default:
		return b.problemResponse(prompt, rng)
	}
}

func (b *mockBank) problemResponse(prompt string, rng *rand.Rand) string {
	lower := strings.ToLower(prompt)
	// Match on the precise topic.Name (which was injected in the prompt)
	// to avoid keyword bleed (e.g. "Circle Theorems" containing "circle"
	// also matches Similar Triangles' display string "similar").
	switch {
	case strings.Contains(lower, "right_triangle_trig"):
		return b.encodeGeom(b.geometryProblems[0], rng) // flagpole
	case strings.Contains(lower, "circle_theorems"):
		return b.encodeGeom(b.geometryProblems[1], rng) // inscribed angle
	case strings.Contains(lower, "similar_triangles"):
		return b.encodeGeom(b.geometryProblems[2], rng) // similar
	case strings.Contains(lower, "linear_equations"):
		return b.encodeAlg(b.algebraProblems[0], rng)
	case strings.Contains(lower, "quadratic_equations"):
		return b.encodeAlg(b.algebraProblems[1], rng)
	case strings.Contains(lower, "systems_of_equations"):
		return b.encodeAlg(b.algebraProblems[2], rng)
	}
	// Fallback: if no precise topic.Name was in the prompt, pick by subject
	if strings.Contains(lower, "algebra") {
		return b.encodeAlg(b.algebraProblems[rng.Intn(len(b.algebraProblems))], rng)
	}
	return b.encodeGeom(b.geometryProblems[rng.Intn(len(b.geometryProblems))], rng)
}

func (b *mockBank) encodeGeom(t geomTemplate, rng *rand.Rand) string {
	out := map[string]any{
		"id":          fmt.Sprintf("geo-%s-%d", t.id, rng.Intn(10000)),
		"stem":        t.stemFmt,
		"answer":      t.answer,
		"numeric":     t.numeric,
		"has_numeric": true,
		"tolerance":   t.tolerance,
		"steps":       t.steps,
		"explanation": t.explanation,
		"skill":       t.skill,
	}
	j, _ := json.Marshal(out)
	return string(j)
}

func (b *mockBank) encodeAlg(t algTemplate, rng *rand.Rand) string {
	out := map[string]any{
		"id":          fmt.Sprintf("alg-%s-%d", t.id, rng.Intn(10000)),
		"stem":        t.stem,
		"answer":      t.answer,
		"numeric":     t.numeric,
		"has_numeric": true,
		"tolerance":   t.tolerance,
		"steps":       t.steps,
		"explanation": t.explanation,
		"skill":       t.skill,
	}
	j, _ := json.Marshal(out)
	return string(j)
}

func (b *mockBank) figureResponse(prompt string, rng *rand.Rand) string {
	lower := strings.ToLower(prompt)
	// Match by topic name first (during generation, the prompt has the topic)
	switch {
	case strings.Contains(lower, "right_triangle_trig") || strings.Contains(prompt, "flagpole"):
		return b.geometryProblems[0].figureSpec
	case strings.Contains(lower, "circle_theorems") || strings.Contains(prompt, "inscribed-angle"):
		return b.geometryProblems[1].figureSpec
	case strings.Contains(lower, "similar_triangles") || strings.Contains(prompt, "similar-triangles"):
		return b.geometryProblems[2].figureSpec
	}
	// Fallback: match by template id substring
	for _, t := range b.geometryProblems {
		if strings.Contains(prompt, t.id) {
			return t.figureSpec
		}
	}
	return b.geometryProblems[0].figureSpec
}

func (b *mockBank) distractorResponse(prompt string, rng *rand.Rand) string {
	lower := strings.ToLower(prompt)
	// Match geometry by topic name
	switch {
	case strings.Contains(lower, "right_triangle_trig") || strings.Contains(prompt, "flagpole"):
		return b.geometryProblems[0].choices
	case strings.Contains(lower, "circle_theorems") || strings.Contains(prompt, "inscribed-angle"):
		return b.geometryProblems[1].choices
	case strings.Contains(lower, "similar_triangles") || strings.Contains(prompt, "similar-triangles"):
		return b.geometryProblems[2].choices
	case strings.Contains(lower, "linear_equations") || strings.Contains(prompt, "linear-eq"):
		return b.algebraProblems[0].choices
	case strings.Contains(lower, "quadratic_equations") || strings.Contains(prompt, "quadratic-1"):
		return b.algebraProblems[1].choices
	case strings.Contains(lower, "systems_of_equations") || strings.Contains(prompt, "system-1"):
		return b.algebraProblems[2].choices
	}
	// Fallback: id match
	for _, t := range b.geometryProblems {
		if strings.Contains(prompt, t.id) {
			return t.choices
		}
	}
	for _, t := range b.algebraProblems {
		if strings.Contains(prompt, t.id) {
			return t.choices
		}
	}
	return b.geometryProblems[0].choices
}

func (b *mockBank) validationResponse(prompt string) string {
	// Always pass validation in the mock — the real validator would actually
	// re-solve the problem. The mock asserts confidence.
	out := map[string]any{
		"ok":         true,
		"confidence": 0.95,
		"issues":     []string{},
		"reasoning":  "Independently solved; answer matches.",
	}
	j, _ := json.Marshal(out)
	return string(j)
}

func (b *mockBank) coachResponse(prompt string) string {
	out := map[string]any{
		"add_weak":      []string{"trig.tan"},
		"remove_weak":   []string{},
		"add_strong":    []string{},
		"remove_strong": []string{},
		"reason":        "Student missed two trig ratio questions in a row.",
	}
	j, _ := json.Marshal(out)
	return string(j)
}
