// Package store provides a SQLite-backed question bank.
//
// Schema:
//
//	questions    -- the question bank (one row per generated+validated question)
//	attempts     -- every student answer submitted (raw event log)
//	scores       -- the grader's verdict per attempt (joined with attempts on PK)
//
// All methods are safe for concurrent use; SQLite handles locking. The driver
// is mattn/go-sqlite3 which requires CGO (a C compiler).
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3" // register sqlite3 driver

	"examen/internal/types"
)

// Bank is the question store. Method signatures are unchanged from the prior
// JSON-backed implementation, so callers in cmd/generate and cmd/serve do not
// need to change.
type Bank struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS questions (
    id            TEXT PRIMARY KEY,
    subject       TEXT NOT NULL,
    topic_name    TEXT NOT NULL,
    topic_display TEXT,
    difficulty    INTEGER NOT NULL,
    skills        TEXT NOT NULL,
    payload       TEXT NOT NULL,
    generated_by  TEXT,
    confidence    REAL,
    generated_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    times_served  INTEGER NOT NULL DEFAULT 0,
    correct_rate  REAL    NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_questions_topic
    ON questions(subject, topic_name, difficulty);
CREATE INDEX IF NOT EXISTS idx_questions_subject
    ON questions(subject);

CREATE TABLE IF NOT EXISTS attempts (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    student_id    TEXT NOT NULL,
    question_id   TEXT NOT NULL,
    selected      INTEGER NOT NULL,
    time_ms       INTEGER NOT NULL,
    submitted_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (question_id) REFERENCES questions(id)
);
CREATE INDEX IF NOT EXISTS idx_attempts_student
    ON attempts(student_id, submitted_at);

CREATE TABLE IF NOT EXISTS scores (
    attempt_id    INTEGER PRIMARY KEY,
    correct       INTEGER NOT NULL,
    correct_index INTEGER NOT NULL,
    misconception TEXT,
    feedback      TEXT,
    skills        TEXT,
    next_skill    TEXT,
    FOREIGN KEY (attempt_id) REFERENCES attempts(id)
);
`

// New opens (or creates) the SQLite database at path and ensures the schema.
func New(path string) (*Bank, error) {
	if path == "" {
		path = "examen.db"
	}
	dsn := fmt.Sprintf("file:%s?_journal=WAL&_busy_timeout=5000&_fk=1", path)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("init schema: %w", err)
	}
	return &Bank{db: db}, nil
}

// Close releases the database handle. Safe to call multiple times.
func (b *Bank) Close() error {
	if b == nil || b.db == nil {
		return nil
	}
	return b.db.Close()
}

// Save is a no-op kept for API compatibility. SQLite commits per-statement.
func (b *Bank) Save() error { return nil }

// Insert stores or replaces a question.
func (b *Bank) Insert(q types.StoredQuestion) types.StoredQuestion {
	if q.GeneratedAt.IsZero() {
		q.GeneratedAt = time.Now()
	}
	payload, err := json.Marshal(q)
	if err != nil {
		panic(fmt.Sprintf("marshal StoredQuestion: %v", err))
	}
	skills, _ := json.Marshal(q.Question.Problem.Topic.Skills)

	_, err = b.db.Exec(`
        INSERT INTO questions
          (id, subject, topic_name, topic_display, difficulty, skills,
           payload, generated_by, confidence, generated_at)
        VALUES (?,?,?,?,?,?,?,?,?,?)
        ON CONFLICT(id) DO UPDATE SET
          subject=excluded.subject,
          topic_name=excluded.topic_name,
          topic_display=excluded.topic_display,
          difficulty=excluded.difficulty,
          skills=excluded.skills,
          payload=excluded.payload,
          generated_by=excluded.generated_by,
          confidence=excluded.confidence,
          generated_at=excluded.generated_at
    `,
		q.Question.ID,
		string(q.Question.Problem.Topic.Subject),
		q.Question.Problem.Topic.Name,
		q.Question.Problem.Topic.Display,
		q.Question.Problem.Topic.Difficulty,
		string(skills),
		string(payload),
		q.GeneratedBy,
		q.Confidence,
		q.GeneratedAt,
	)
	if err != nil {
		panic(fmt.Sprintf("insert question: %v", err))
	}
	return q
}

// Find returns one question matching the topic and difficulty, avoiding the
// given IDs. Falls back to any unseen question in the same subject if no
// exact-topic match is available.
func (b *Bank) Find(topic types.Topic, difficulty int, avoid []string) (types.Question, error) {
	if q, ok := b.findOne(topic, difficulty, avoid, true); ok {
		return q, nil
	}
	if q, ok := b.findOne(topic, difficulty, avoid, false); ok {
		return q, nil
	}
	return types.Question{}, fmt.Errorf("no question for topic %s", topic.Name)
}

func (b *Bank) findOne(topic types.Topic, difficulty int, avoid []string, strict bool) (types.Question, bool) {
	args := []any{string(topic.Subject)}
	q := "SELECT payload FROM questions WHERE subject = ?"
	if strict {
		q += " AND topic_name = ?"
		args = append(args, topic.Name)
	}
	if len(avoid) > 0 {
		placeholders := strings.Repeat("?,", len(avoid))
		placeholders = placeholders[:len(placeholders)-1]
		q += fmt.Sprintf(" AND id NOT IN (%s)", placeholders)
		for _, id := range avoid {
			args = append(args, id)
		}
	}
	q += " ORDER BY times_served ASC, RANDOM() LIMIT 1"

	row := b.db.QueryRow(q, args...)
	var payload string
	if err := row.Scan(&payload); err != nil {
		if err == sql.ErrNoRows {
			return types.Question{}, false
		}
		return types.Question{}, false
	}
	var stored types.StoredQuestion
	if err := json.Unmarshal([]byte(payload), &stored); err != nil {
		return types.Question{}, false
	}
	_, _ = b.db.Exec("UPDATE questions SET times_served = times_served + 1 WHERE id = ?",
		stored.Question.ID)
	return stored.Question, true
}

// Get returns a question by ID. Used by the grader.
func (b *Bank) Get(id string) (types.Question, error) {
	row := b.db.QueryRow("SELECT payload FROM questions WHERE id = ?", id)
	var payload string
	if err := row.Scan(&payload); err != nil {
		if err == sql.ErrNoRows {
			return types.Question{}, fmt.Errorf("question %s not found", id)
		}
		return types.Question{}, err
	}
	var stored types.StoredQuestion
	if err := json.Unmarshal([]byte(payload), &stored); err != nil {
		return types.Question{}, err
	}
	return stored.Question, nil
}

// Count returns the total number of questions in the bank.
func (b *Bank) Count() int {
	row := b.db.QueryRow("SELECT COUNT(*) FROM questions")
	var n int
	_ = row.Scan(&n)
	return n
}

// CountBySubject returns counts per subject.
func (b *Bank) CountBySubject() map[types.Subject]int {
	out := make(map[types.Subject]int)
	rows, err := b.db.Query("SELECT subject, COUNT(*) FROM questions GROUP BY subject")
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var s string
		var n int
		if err := rows.Scan(&s, &n); err == nil {
			out[types.Subject(s)] = n
		}
	}
	return out
}

// RecordAttempt logs a student attempt and its score in one transaction.
func (b *Bank) RecordAttempt(att types.Attempt, score types.Score) {
	tx, err := b.db.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()

	res, err := tx.Exec(`
        INSERT INTO attempts (student_id, question_id, selected, time_ms, submitted_at)
        VALUES (?, ?, ?, ?, ?)`,
		att.StudentID, att.QuestionID, att.Selected, att.TimeMs, att.Submitted)
	if err != nil {
		return
	}
	attID, _ := res.LastInsertId()

	skills, _ := json.Marshal(score.Skills)
	correctInt := 0
	if score.Correct {
		correctInt = 1
	}
	_, err = tx.Exec(`
        INSERT INTO scores (attempt_id, correct, correct_index, misconception,
                            feedback, skills, next_skill)
        VALUES (?, ?, ?, ?, ?, ?, ?)`,
		attID, correctInt, score.CorrectIndex, score.Misconception,
		score.Feedback, string(skills), score.NextSkill)
	if err != nil {
		return
	}

	// Update the question's correct_rate as an exponential moving average.
	_, _ = tx.Exec(`
        UPDATE questions SET correct_rate = (correct_rate * 0.9 + ? * 0.1)
        WHERE id = ?`, correctInt, att.QuestionID)

	_ = tx.Commit()
}

// AllQuestions returns a snapshot of the bank.
func (b *Bank) AllQuestions() []types.StoredQuestion {
	rows, err := b.db.Query(`SELECT payload FROM questions ORDER BY id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []types.StoredQuestion
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			continue
		}
		var stored types.StoredQuestion
		if err := json.Unmarshal([]byte(payload), &stored); err != nil {
			continue
		}
		out = append(out, stored)
	}
	return out
}

// AttemptsFor returns recent attempts for a student, newest first, up to limit.
func (b *Bank) AttemptsFor(studentID string, limit int) []AttemptWithScore {
	if limit <= 0 {
		limit = 50
	}
	rows, err := b.db.Query(`
        SELECT a.id, a.student_id, a.question_id, a.selected, a.time_ms,
               a.submitted_at, s.correct, s.correct_index, s.misconception,
               s.feedback, s.skills, s.next_skill
        FROM attempts a
        LEFT JOIN scores s ON s.attempt_id = a.id
        WHERE a.student_id = ?
        ORDER BY a.submitted_at DESC
        LIMIT ?`, studentID, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var out []AttemptWithScore
	for rows.Next() {
		var aws AttemptWithScore
		var skillsJSON sql.NullString
		var correct sql.NullInt64
		var correctIdx sql.NullInt64
		var misc, fb, nextSkill sql.NullString
		if err := rows.Scan(
			&aws.AttemptID, &aws.Attempt.StudentID, &aws.Attempt.QuestionID,
			&aws.Attempt.Selected, &aws.Attempt.TimeMs, &aws.Attempt.Submitted,
			&correct, &correctIdx, &misc, &fb, &skillsJSON, &nextSkill,
		); err != nil {
			continue
		}
		aws.Score.StudentID = aws.Attempt.StudentID
		aws.Score.QuestionID = aws.Attempt.QuestionID
		aws.Score.Selected = aws.Attempt.Selected
		aws.Score.TimeMs = aws.Attempt.TimeMs
		if correct.Valid {
			aws.Score.Correct = correct.Int64 == 1
		}
		if correctIdx.Valid {
			aws.Score.CorrectIndex = int(correctIdx.Int64)
		}
		if misc.Valid {
			aws.Score.Misconception = misc.String
		}
		if fb.Valid {
			aws.Score.Feedback = fb.String
		}
		if nextSkill.Valid {
			aws.Score.NextSkill = nextSkill.String
		}
		if skillsJSON.Valid && skillsJSON.String != "" {
			_ = json.Unmarshal([]byte(skillsJSON.String), &aws.Score.Skills)
		}
		out = append(out, aws)
	}
	return out
}

// AttemptWithScore is a join of an attempt and its grader output.
type AttemptWithScore struct {
	AttemptID int64
	Attempt   types.Attempt
	Score     types.Score
}
