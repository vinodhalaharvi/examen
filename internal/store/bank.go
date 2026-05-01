// Package store provides a simple JSON-file-backed question bank.
// For a hackathon this is plenty; production would swap in Postgres or SQLite
// with the same method signatures (no interfaces, just struct methods).
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"examen/internal/types"
)

// Bank is the question store. Thread-safe.
type Bank struct {
	path      string
	mu        sync.RWMutex
	questions map[string]types.StoredQuestion
	attempts  []types.Attempt
	scores    []types.Score
}

func New(path string) (*Bank, error) {
	b := &Bank{
		path:      path,
		questions: make(map[string]types.StoredQuestion),
	}
	if _, err := os.Stat(path); err == nil {
		if err := b.load(); err != nil {
			return nil, err
		}
	}
	return b, nil
}

type fileFormat struct {
	Questions []types.StoredQuestion `json:"questions"`
	Attempts  []types.Attempt        `json:"attempts"`
	Scores    []types.Score          `json:"scores"`
}

func (b *Bank) load() error {
	data, err := os.ReadFile(b.path)
	if err != nil {
		return err
	}
	var ff fileFormat
	if err := json.Unmarshal(data, &ff); err != nil {
		return err
	}
	for _, q := range ff.Questions {
		b.questions[q.Question.ID] = q
	}
	b.attempts = ff.Attempts
	b.scores = ff.Scores
	return nil
}

// Save persists the bank to disk. Caller should call after batched writes.
func (b *Bank) Save() error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	qs := make([]types.StoredQuestion, 0, len(b.questions))
	for _, q := range b.questions {
		qs = append(qs, q)
	}
	sort.Slice(qs, func(i, j int) bool { return qs[i].Question.ID < qs[j].Question.ID })
	ff := fileFormat{Questions: qs, Attempts: b.attempts, Scores: b.scores}
	data, err := json.MarshalIndent(ff, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(b.path, data, 0644)
}

// Insert stores or replaces a question.
func (b *Bank) Insert(q types.StoredQuestion) types.StoredQuestion {
	b.mu.Lock()
	defer b.mu.Unlock()
	if q.GeneratedAt.IsZero() {
		q.GeneratedAt = time.Now()
	}
	b.questions[q.Question.ID] = q
	return q
}

// Find returns one question matching the topic and difficulty, avoiding the
// given IDs. Returns ErrNotFound if no match.
func (b *Bank) Find(topic types.Topic, difficulty int, avoid []string) (types.Question, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	avoidSet := make(map[string]bool, len(avoid))
	for _, id := range avoid {
		avoidSet[id] = true
	}
	for _, q := range b.questions {
		if q.Question.Problem.Topic.Subject != topic.Subject {
			continue
		}
		if q.Question.Problem.Topic.Name != topic.Name {
			continue
		}
		if avoidSet[q.Question.ID] {
			continue
		}
		return q.Question, nil
	}
	// Relaxed: any question in the same subject, not seen
	for _, q := range b.questions {
		if q.Question.Problem.Topic.Subject != topic.Subject {
			continue
		}
		if avoidSet[q.Question.ID] {
			continue
		}
		return q.Question, nil
	}
	return types.Question{}, fmt.Errorf("no question for topic %s", topic.Name)
}

// Get returns a question by ID. Used by the grader.
func (b *Bank) Get(id string) (types.Question, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if q, ok := b.questions[id]; ok {
		return q.Question, nil
	}
	return types.Question{}, fmt.Errorf("question %s not found", id)
}

// Count returns the total number of questions in the bank.
func (b *Bank) Count() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.questions)
}

// CountBySubject returns counts per subject.
func (b *Bank) CountBySubject() map[types.Subject]int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make(map[types.Subject]int)
	for _, q := range b.questions {
		out[q.Question.Problem.Topic.Subject]++
	}
	return out
}

// RecordAttempt logs a student attempt and its score.
func (b *Bank) RecordAttempt(att types.Attempt, score types.Score) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.attempts = append(b.attempts, att)
	b.scores = append(b.scores, score)
}

// AllQuestions returns a snapshot of the bank.
func (b *Bank) AllQuestions() []types.StoredQuestion {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]types.StoredQuestion, 0, len(b.questions))
	for _, q := range b.questions {
		out = append(out, q)
	}
	return out
}
