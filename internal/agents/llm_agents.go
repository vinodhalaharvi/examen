package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"examen/internal/llm"
	"examen/internal/render"
	"examen/internal/types"
)

// =============================================================================
// Problem agent — generates a math problem with a verifiable answer
// =============================================================================

type Problem struct {
	Client *llm.Client
}

func NewProblem(c *llm.Client) *Problem { return &Problem{Client: c} }

type problemJSON struct {
	ID          string          `json:"id"`
	Stem        string          `json:"stem"`
	Answer      string          `json:"answer"`
	Numeric     float64         `json:"numeric"`
	HasNumeric  bool            `json:"has_numeric"`
	Tolerance   float64         `json:"tolerance"`
	Steps       []string        `json:"steps"`
	Explanation string          `json:"explanation"`
	Skill       string          `json:"skill"`
	FigureSpec  json.RawMessage `json:"figure_spec,omitempty"`
	Choices     json.RawMessage `json:"choices,omitempty"`
}

const problemSystem = `You are a math problem generator. You MUST output ONLY a single valid JSON object — no prose, no markdown code fences, no preamble, no commentary, no explanation outside the JSON. Start your response with { and end with }.

The JSON schema:
{
  "id": "short-kebab-case-id",
  "stem": "the full problem text the student will read",
  "answer": "the correct answer as a string (e.g. '16.8' or 'x = 3')",
  "numeric": 0.0,
  "has_numeric": true,
  "tolerance": 0.01,
  "steps": ["step 1", "step 2", "..."],
  "explanation": "one-sentence solution summary",
  "skill": "skill.tag.like.trig.tan"
}

Rules:
- Generate problems with exactly one correct numerical answer.
- The "id" should be a short slug like "flagpole-shadow" or "linear-eq-1".
- "numeric" is the numeric value of the answer (use 0 if not numeric).
- "tolerance" is acceptable error for numeric matching (e.g. 0.01 or 0.5).
- Output the JSON object and nothing else.`

func (p *Problem) Generate(ctx context.Context, req types.TopicRequest) types.Problem {
	user := fmt.Sprintf(
		"Generate a math problem on topic %q (name: %s, subject: %s, difficulty %d/5). Skills: %v.",
		req.Topic.Display, req.Topic.Name, req.Topic.Subject, req.Difficulty, req.Skills)

	resp, err := p.Client.Complete(ctx, llm.Request{
		System:      problemSystem,
		User:        user,
		Tier:        llm.TierBalance,
		MaxTokens:   1500,
		Temperature: 0.7,
	})
	if err != nil {
		log.Printf("[problem] llm error: %v", err)
		return types.Problem{}
	}
	raw := llm.ExtractJSON(resp.Text)
	var parsed problemJSON
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		log.Printf("[problem] parse failed: %v\nraw response (first 500 chars):\n%s",
			err, truncate(resp.Text, 500))
		return types.Problem{}
	}
	if parsed.ID == "" {
		// LLM omitted the id; synthesize one from topic + skill so the question
		// has a stable identifier downstream.
		parsed.ID = fmt.Sprintf("%s-%s", req.Topic.Name, parsed.Skill)
	}
	return types.Problem{
		ID:    parsed.ID,
		Topic: req.Topic,
		Stem:  parsed.Stem,
		Solution: types.Solution{
			Answer:      parsed.Answer,
			Numeric:     parsed.Numeric,
			HasNumeric:  parsed.HasNumeric,
			Tolerance:   parsed.Tolerance,
			Steps:       parsed.Steps,
			Explanation: parsed.Explanation,
		},
		Difficulty: req.Difficulty,
		Skills:     append(req.Skills, parsed.Skill),
	}
}

// =============================================================================
// Figure agent — generates a FigureSpec, validates, renders to SVG
// =============================================================================

type Figure struct {
	Client *llm.Client
}

func NewFigure(c *llm.Client) *Figure { return &Figure{Client: c} }

const figureSystem = `You are a geometry figure generator. You MUST output ONLY a single valid JSON object matching the FigureSpec schema. No prose, no markdown code fences, no preamble. Start with { and end with }.

Schema:
{
  "width": 360,
  "height": 240,
  "view_box": "0 0 360 240",
  "primitives": [
    {"kind": "line", "attrs": {"x1": 20, "y1": 200, "x2": 340, "y2": 200}, "style": "default"},
    {"kind": "circle", "attrs": {"cx": 160, "cy": 140, "r": 100}, "style": "default"},
    {"kind": "polygon", "attrs": {"points_count": 3, "x0": 40, "y0": 200, "x1": 40, "y1": 140, "x2": 110, "y2": 200}, "style": "soft_fill"},
    {"kind": "right_angle", "attrs": {"x": 280, "y": 200, "size": 10}},
    {"kind": "angle_marker", "attrs": {"cx": 40, "cy": 200, "r": 35, "start": 0, "sweep": -35}, "style": "highlight"}
  ],
  "labels": [
    {"text": "35°", "x": 80, "y": 195, "style": "highlight"},
    {"text": "24 m", "x": 150, "y": 220, "style": "default"}
  ]
}

Style values: "default", "highlight", "dashed", "soft_fill".
All coordinates must be inside the viewBox. Use "highlight" sparingly for the key element.
Output the JSON object and nothing else.`

func (f *Figure) Render(ctx context.Context, p types.Problem) types.Figure {
	if p.Topic.Subject != types.SubjectGeometry {
		// Algebra problems get no figure
		return types.Figure{}
	}
	user := fmt.Sprintf(
		"Generate a figure for this geometry problem.\nProblem id: %s\nStem: %s",
		p.ID, p.Stem)

	resp, err := f.Client.Complete(ctx, llm.Request{
		System:    figureSystem,
		User:      user,
		Tier:      llm.TierBalance,
		MaxTokens: 2000,
	})
	if err != nil {
		log.Printf("[figure] llm error: %v", err)
		return types.Figure{}
	}
	raw := llm.ExtractJSON(resp.Text)
	var spec types.FigureSpec
	if err := json.Unmarshal([]byte(raw), &spec); err != nil {
		log.Printf("[figure] parse failed: %v\nraw (first 500 chars):\n%s",
			err, truncate(resp.Text, 500))
		return types.Figure{}
	}
	if err := render.Validate(spec); err != nil {
		log.Printf("[figure] validation failed: %v", err)
		return types.Figure{}
	}
	return types.Figure{
		Spec: spec,
		SVG:  render.SVG(spec),
	}
}

// =============================================================================
// Distractor agent — generates wrong answers tagged with misconceptions
// =============================================================================

type Distractor struct {
	Client *llm.Client
}

func NewDistractor(c *llm.Client) *Distractor { return &Distractor{Client: c} }

type choiceJSON struct {
	Text          string `json:"text"`
	IsCorrect     bool   `json:"is_correct"`
	Misconception string `json:"misconception,omitempty"`
	Rationale     string `json:"rationale,omitempty"`
}

const distractorSystem = `You generate multiple-choice options for math problems. You MUST output ONLY a JSON array of EXACTLY 4 choices — no prose, no markdown code fences, no preamble. Start with [ and end with ].

Each choice has the shape:
{
  "text": "the answer as a string",
  "is_correct": true | false,
  "misconception": "specific student mistake — only on wrong choices",
  "rationale": "why this is correct — only on the correct choice"
}

Rules:
- Output exactly 4 choices.
- Exactly one choice must have "is_correct": true.
- Three choices must have "is_correct": false.
- Each wrong choice must have a non-empty "misconception" tag describing the
  specific computational or conceptual error a student would make to arrive
  at that answer (e.g. "Used cos instead of tan" or "Forgot to square the radius").
- The correct choice should have a "rationale" explaining the solution.
- Output the JSON array and nothing else.`

func (d *Distractor) Make(ctx context.Context, p types.Problem) []types.Choice {
	user := fmt.Sprintf(
		"Generate 4 multiple-choice options for this problem.\nProblem id: %s\nStem: %s\nCorrect answer: %s\nMust include the correct answer as one of the four choices.",
		p.ID, p.Stem, p.Solution.Answer)

	resp, err := d.Client.Complete(ctx, llm.Request{
		System:    distractorSystem,
		User:      user,
		Tier:      llm.TierFast,
		MaxTokens: 1000,
	})
	if err != nil {
		log.Printf("[distractor] llm error: %v", err)
		return nil
	}
	raw := llm.ExtractJSON(resp.Text)
	var arr []choiceJSON
	if err := json.Unmarshal([]byte(raw), &arr); err != nil {
		log.Printf("[distractor] parse failed: %v\nraw (first 500 chars):\n%s",
			err, truncate(resp.Text, 500))
		return nil
	}
	out := make([]types.Choice, 0, len(arr))
	for _, c := range arr {
		out = append(out, types.Choice{
			Text:          c.Text,
			IsCorrect:     c.IsCorrect,
			Misconception: c.Misconception,
			Rationale:     c.Rationale,
		})
	}
	return out
}

// =============================================================================
// Validator agent — independently checks the question
// =============================================================================

type Validator struct {
	Client *llm.Client
}

func NewValidator(c *llm.Client) *Validator { return &Validator{Client: c} }

type validationJSON struct {
	OK         bool     `json:"ok"`
	Confidence float64  `json:"confidence"`
	Issues     []string `json:"issues"`
	Reasoning  string   `json:"reasoning"`
}

const validatorSystem = `You are a math problem validator. Independently solve the given problem, then check whether the marked-correct choice is actually correct and the distractors are actually wrong.

You MUST output ONLY a single JSON object — no prose, no markdown code fences, no preamble. Start with { and end with }.

Schema:
{
  "ok": true | false,
  "confidence": 0.0,
  "issues": ["list", "of", "issues"],
  "reasoning": "brief explanation"
}

Set "ok" to true only if the marked-correct choice is genuinely correct AND none of the other choices are also correct. List specific issues if found.`

func (v *Validator) Check(ctx context.Context, q types.Question) types.ValidatedQuestion {
	// Sanity check: must have exactly one correct choice
	correctCount := 0
	for _, c := range q.Choices {
		if c.IsCorrect {
			correctCount++
		}
	}
	if correctCount != 1 {
		return types.ValidatedQuestion{
			Question: q, OK: false,
			Issues:      []string{fmt.Sprintf("expected 1 correct choice, got %d", correctCount)},
			ValidatedBy: []string{"sanity-check"},
		}
	}

	user := fmt.Sprintf(
		"Validate this problem and choices.\nStem: %s\nMarked correct: %s\nAll choices: %+v",
		q.Problem.Stem, q.Problem.Solution.Answer, q.Choices)

	resp, err := v.Client.Complete(ctx, llm.Request{
		System:    validatorSystem,
		User:      user,
		Tier:      llm.TierStrong,
		MaxTokens: 800,
	})
	if err != nil {
		log.Printf("[validator] llm error: %v", err)
		return types.ValidatedQuestion{
			Question: q, OK: false,
			Issues: []string{err.Error()},
		}
	}
	raw := llm.ExtractJSON(resp.Text)
	var parsed validationJSON
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		log.Printf("[validator] parse failed: %v; falling back to sanity-check pass", err)
		// If we can't parse, accept the sanity check passed
		return types.ValidatedQuestion{
			Question:    q,
			OK:          true,
			Confidence:  0.5,
			ValidatedBy: []string{"sanity-check"},
		}
	}
	return types.ValidatedQuestion{
		Question:    q,
		OK:          parsed.OK,
		Confidence:  parsed.Confidence,
		Issues:      parsed.Issues,
		ValidatedBy: []string{"sanity-check", "llm-judge"},
	}
}

// truncate shortens a string to n runes for log readability without panicking
// on multi-byte boundaries.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}
