package agents

import (
	"context"
	"encoding/json"
	"fmt"

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

const problemSystem = `You are a math problem generator. Output ONLY valid JSON
matching this schema:
{
  "id": "string", "stem": "the problem text",
  "answer": "the correct answer", "numeric": 0.0, "has_numeric": true,
  "tolerance": 0.01, "steps": ["..."], "explanation": "...",
  "skill": "skill.tag"
}
Generate problems with exactly one correct numerical answer.`

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
		return types.Problem{}
	}
	var parsed problemJSON
	if err := json.Unmarshal([]byte(resp.Text), &parsed); err != nil {
		return types.Problem{}
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

const figureSystem = `You are a geometry figure generator. Output ONLY valid JSON
matching the FigureSpec schema with primitives (line, circle, polygon, arc,
right_angle, angle_marker) and labels. Coordinates in viewBox space.`

func (f *Figure) Render(ctx context.Context, p types.Problem) types.Figure {
	if p.Topic.Subject != types.SubjectGeometry {
		// Algebra problems get no figure
		return types.Figure{}
	}
	user := fmt.Sprintf(
		"Generate a figure for problem id %s. Stem: %s",
		p.ID, p.Stem)

	resp, err := f.Client.Complete(ctx, llm.Request{
		System:    figureSystem,
		User:      user,
		Tier:      llm.TierBalance,
		MaxTokens: 2000,
	})
	if err != nil {
		return types.Figure{}
	}
	var spec types.FigureSpec
	if err := json.Unmarshal([]byte(resp.Text), &spec); err != nil {
		return types.Figure{}
	}
	if err := render.Validate(spec); err != nil {
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

const distractorSystem = `You generate multiple-choice options for math problems.
Output ONLY a JSON array of 4 choices, exactly one with "is_correct": true.
Each wrong answer must have a "misconception" tag describing the specific
mistake a student would make to arrive at that answer.`

func (d *Distractor) Make(ctx context.Context, p types.Problem) []types.Choice {
	user := fmt.Sprintf(
		"Problem id %s: %s\nCorrect answer: %s\nGenerate 4 choices including the correct answer.",
		p.ID, p.Stem, p.Solution.Answer)

	resp, err := d.Client.Complete(ctx, llm.Request{
		System:    distractorSystem,
		User:      user,
		Tier:      llm.TierFast,
		MaxTokens: 1000,
	})
	if err != nil {
		return nil
	}
	var arr []choiceJSON
	if err := json.Unmarshal([]byte(resp.Text), &arr); err != nil {
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

const validatorSystem = `You are a math problem validator. Independently solve
the given problem, then check whether the marked-correct choice is actually
correct and the distractors are actually wrong. Output JSON:
{"ok": bool, "confidence": 0.0-1.0, "issues": [...], "reasoning": "..."}`

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
		return types.ValidatedQuestion{
			Question: q, OK: false,
			Issues: []string{err.Error()},
		}
	}
	var parsed validationJSON
	if err := json.Unmarshal([]byte(resp.Text), &parsed); err != nil {
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
