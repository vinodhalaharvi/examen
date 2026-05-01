// Package types defines the domain model that flows through the agent pipelines.
package types

import "time"

type Subject string

const (
	SubjectGeometry Subject = "geometry"
	SubjectAlgebra  Subject = "algebra"
)

type Topic struct {
	Subject    Subject  `json:"subject"`
	Name       string   `json:"name"`
	Display    string   `json:"display"`
	Skills     []string `json:"skills"`
	Difficulty int      `json:"difficulty"`
}

type StudentProfile struct {
	StudentID    string   `json:"student_id"`
	WeakSkills   []string `json:"weak_skills"`
	StrongSkills []string `json:"strong_skills"`
	SeenIDs      []string `json:"seen_ids"`
	Subject      Subject  `json:"subject"`
	TargetLevel  int      `json:"target_level"`
}

type TopicRequest struct {
	StudentID  string   `json:"student_id"`
	Topic      Topic    `json:"topic"`
	Difficulty int      `json:"difficulty"`
	Skills     []string `json:"skills"`
	Avoid      []string `json:"avoid"`
}

type Solution struct {
	Answer      string   `json:"answer"`
	Numeric     float64  `json:"numeric"`
	HasNumeric  bool     `json:"has_numeric"`
	Tolerance   float64  `json:"tolerance"`
	Steps       []string `json:"steps"`
	Explanation string   `json:"explanation"`
}

type Problem struct {
	ID         string   `json:"id"`
	Topic      Topic    `json:"topic"`
	Stem       string   `json:"stem"`
	Solution   Solution `json:"solution"`
	Difficulty int      `json:"difficulty"`
	Skills     []string `json:"skills"`
}

type FigureSpec struct {
	Width      int            `json:"width"`
	Height     int            `json:"height"`
	ViewBox    string         `json:"view_box"`
	Primitives []SVGPrimitive `json:"primitives"`
	Labels     []FigureLabel  `json:"labels"`
}

type SVGPrimitive struct {
	Kind  string             `json:"kind"`
	Attrs map[string]float64 `json:"attrs"`
	Style string             `json:"style"`
	ID    string             `json:"id,omitempty"`
}

type FigureLabel struct {
	Text  string  `json:"text"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	Style string  `json:"style"`
}

type Figure struct {
	Spec FigureSpec `json:"spec"`
	SVG  string     `json:"svg"`
}

type Choice struct {
	Text          string `json:"text"`
	IsCorrect     bool   `json:"is_correct"`
	Misconception string `json:"misconception,omitempty"`
	Rationale     string `json:"rationale,omitempty"`
}

type Question struct {
	ID        string   `json:"id"`
	Problem   Problem  `json:"problem"`
	Figure    Figure   `json:"figure"`
	Choices   []Choice `json:"choices"`
	NextSkill string   `json:"next_skill"`
}

type ValidatedQuestion struct {
	Question    Question `json:"question"`
	OK          bool     `json:"ok"`
	Confidence  float64  `json:"confidence"`
	Issues      []string `json:"issues"`
	ValidatedBy []string `json:"validated_by"`
}

type Attempt struct {
	StudentID  string    `json:"student_id"`
	QuestionID string    `json:"question_id"`
	Selected   int       `json:"selected"`
	TimeMs     int       `json:"time_ms"`
	Submitted  time.Time `json:"submitted"`
}

type Score struct {
	StudentID     string   `json:"student_id"`
	QuestionID    string   `json:"question_id"`
	Correct       bool     `json:"correct"`
	Selected      int      `json:"selected"`
	CorrectIndex  int      `json:"correct_index"`
	Misconception string   `json:"misconception,omitempty"`
	Skills        []string `json:"skills"`
	Feedback      string   `json:"feedback"`
	NextSkill     string   `json:"next_skill"`
	TimeMs        int      `json:"time_ms"`
}

type CoachUpdate struct {
	StudentID    string   `json:"student_id"`
	AddWeak      []string `json:"add_weak"`
	RemoveWeak   []string `json:"remove_weak"`
	AddStrong    []string `json:"add_strong"`
	RemoveStrong []string `json:"remove_strong"`
	Reason       string   `json:"reason"`
}

type StoredQuestion struct {
	Question    Question  `json:"question"`
	GeneratedAt time.Time `json:"generated_at"`
	GeneratedBy string    `json:"generated_by"`
	ValidatedBy []string  `json:"validated_by"`
	TimesServed int       `json:"times_served"`
	CorrectRate float64   `json:"correct_rate"`
}

type GenerationRequest struct {
	Topic      Topic  `json:"topic"`
	Difficulty int    `json:"difficulty"`
	Count      int    `json:"count"`
	RunID      string `json:"run_id"`
}
