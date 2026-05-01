package agents

import (
	"context"
	"encoding/json"
	"fmt"

	"examen/internal/llm"
	"examen/internal/types"
)

// =============================================================================
// Grader — pure logic, no LLM. Compares attempt to correct answer, identifies
// the misconception based on the chosen wrong distractor's tag.
// =============================================================================

type Grader struct {
	store QuestionLookup
}

// QuestionLookup is the slice of the store interface the grader needs.
// Function-typed to avoid the interface pattern.
type QuestionLookup func(id string) (types.Question, error)

func NewGrader(lookup QuestionLookup) *Grader {
	return &Grader{store: lookup}
}

func (g *Grader) Grade(att types.Attempt) types.Score {
	q, err := g.store(att.QuestionID)
	if err != nil {
		return types.Score{
			StudentID:  att.StudentID,
			QuestionID: att.QuestionID,
			Feedback:   "question not found",
		}
	}
	correctIdx := -1
	for i, c := range q.Choices {
		if c.IsCorrect {
			correctIdx = i
			break
		}
	}
	if att.Selected < 0 || att.Selected >= len(q.Choices) {
		return types.Score{
			StudentID:    att.StudentID,
			QuestionID:   att.QuestionID,
			CorrectIndex: correctIdx,
			Feedback:     "invalid selection",
		}
	}
	chosen := q.Choices[att.Selected]
	correct := chosen.IsCorrect

	score := types.Score{
		StudentID:    att.StudentID,
		QuestionID:   att.QuestionID,
		Correct:      correct,
		Selected:     att.Selected,
		CorrectIndex: correctIdx,
		Skills:       q.Problem.Skills,
		TimeMs:       att.TimeMs,
		NextSkill:    q.NextSkill,
	}
	if correct {
		score.Feedback = q.Choices[correctIdx].Rationale
	} else {
		score.Misconception = chosen.Misconception
		score.Feedback = fmt.Sprintf("Correct answer: %s. %s",
			q.Choices[correctIdx].Text, q.Choices[correctIdx].Rationale)
	}
	return score
}

// =============================================================================
// Coach — observes a window of recent scores, emits profile updates
// =============================================================================

type Coach struct {
	Client *llm.Client
}

func NewCoach(c *llm.Client) *Coach { return &Coach{Client: c} }

type coachJSON struct {
	AddWeak      []string `json:"add_weak"`
	RemoveWeak   []string `json:"remove_weak"`
	AddStrong    []string `json:"add_strong"`
	RemoveStrong []string `json:"remove_strong"`
	Reason       string   `json:"reason"`
}

const coachSystem = `You analyze a student's recent answers and identify
patterns. Output ONLY JSON adjusting the student's weak/strong skill lists.`

func (c *Coach) Recommend(ctx context.Context, window []types.Score) types.CoachUpdate {
	if len(window) == 0 {
		return types.CoachUpdate{}
	}
	studentID := window[0].StudentID

	// Collect misconception patterns
	missedSkills := make(map[string]int)
	correctSkills := make(map[string]int)
	for _, s := range window {
		for _, skill := range s.Skills {
			if s.Correct {
				correctSkills[skill]++
			} else {
				missedSkills[skill]++
			}
		}
	}

	user := fmt.Sprintf(
		"Recent scores: %d total, %d correct.\nMissed skills: %v\nCorrect skills: %v\nWhat should we focus on?",
		len(window), countCorrect(window), missedSkills, correctSkills)

	resp, err := c.Client.Complete(ctx, llm.Request{
		System:    coachSystem,
		User:      user,
		Tier:      llm.TierFast,
		MaxTokens: 400,
	})
	if err != nil {
		// Fallback: rule-based update
		return rulebasedCoach(studentID, missedSkills, correctSkills)
	}
	var parsed coachJSON
	if err := json.Unmarshal([]byte(resp.Text), &parsed); err != nil {
		return rulebasedCoach(studentID, missedSkills, correctSkills)
	}
	return types.CoachUpdate{
		StudentID:    studentID,
		AddWeak:      parsed.AddWeak,
		RemoveWeak:   parsed.RemoveWeak,
		AddStrong:    parsed.AddStrong,
		RemoveStrong: parsed.RemoveStrong,
		Reason:       parsed.Reason,
	}
}

func countCorrect(window []types.Score) int {
	n := 0
	for _, s := range window {
		if s.Correct {
			n++
		}
	}
	return n
}

func rulebasedCoach(sid string, missed, correct map[string]int) types.CoachUpdate {
	upd := types.CoachUpdate{StudentID: sid, Reason: "rule-based"}
	for skill, n := range missed {
		if n >= 2 {
			upd.AddWeak = append(upd.AddWeak, skill)
		}
	}
	for skill, n := range correct {
		if n >= 3 {
			upd.AddStrong = append(upd.AddStrong, skill)
			upd.RemoveWeak = append(upd.RemoveWeak, skill)
		}
	}
	return upd
}
