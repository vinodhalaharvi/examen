// cmd/serve runs the live student-facing web server.
//
// Composes two pipelines:
//   1. Serving:   StudentProfile -> Question (curriculum -> lookup)
//   2. Grading:   Attempt -> Score (grader, with coach observing via Tee)
//
// The HTTP handlers push values into channels and read results out.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"examen/internal/agents"
	"examen/internal/channels"
	"examen/internal/llm"
	"examen/internal/store"
	"examen/internal/types"
)

func main() {
	var (
		bankPath = flag.String("bank", "data/examen.db", "question bank database path (SQLite)")
		addr     = flag.String("addr", ":8080", "listen address")
		webDir   = flag.String("web", "web", "static web files")
		useReal  = flag.Bool("real", false, "use real Claude API for the coach")
	)
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	bank, err := store.New(*bankPath)
	if err != nil {
		log.Fatal(err)
	}
	defer bank.Close()
	if bank.Count() == 0 {
		log.Fatal("question bank is empty — run `go run ./cmd/generate` first")
	}
	log.Printf("[serve] loaded %d questions from %s", bank.Count(), *bankPath)

	client := buildClient(*useReal)

	// === Build agents ===
	curriculum := agents.NewCurriculum(time.Now().UnixNano())
	grader := agents.NewGrader(bank.Get)
	coach := agents.NewCoach(client)

	// === Build the server ===
	srv := newServer(ctx, bank, curriculum, grader, coach)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/question/next", srv.handleNext)
	mux.HandleFunc("/api/answer/submit", srv.handleSubmit)
	mux.HandleFunc("/api/session/state", srv.handleState)
	mux.Handle("/", http.FileServer(http.Dir(*webDir)))

	server := &http.Server{
		Addr:         *addr,
		Handler:      logMW(mux),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	go func() {
		<-ctx.Done()
		log.Println("[serve] shutting down...")
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutCtx)
	}()

	log.Printf("[serve] listening on %s — open http://localhost%s/", *addr, *addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func buildClient(useReal bool) *llm.Client {
	if useReal {
		c, err := llm.NewClaude()
		if err != nil {
			log.Printf("[warn] failed Claude init: %v; using mock", err)
			return llm.NewMock()
		}
		return c
	}
	return llm.NewMock()
}

// =============================================================================
// SERVER
// =============================================================================

type server struct {
	ctx        context.Context
	bank       *store.Bank
	curriculum *agents.Curriculum
	grader     *agents.Grader
	coach      *agents.Coach

	mu       sync.Mutex
	sessions map[string]*session
}

type session struct {
	studentID string
	profile   types.StudentProfile

	// Channels for the question pipeline
	profileIn chan types.StudentProfile
	questions <-chan types.Question

	// Channels for the grading pipeline
	attemptIn chan types.Attempt
	scores    <-chan types.Score

	// Coach observation
	coachInput chan types.Score
}

func newServer(ctx context.Context, bank *store.Bank, c *agents.Curriculum,
	g *agents.Grader, coach *agents.Coach,
) *server {
	return &server{
		ctx: ctx, bank: bank,
		curriculum: c, grader: g, coach: coach,
		sessions: make(map[string]*session),
	}
}

// getSession finds or creates a session for the student. Each session has its
// own pipeline instance — the agent graph runs continuously per student.
func (s *server) getSession(studentID string) *session {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[studentID]; ok {
		return sess
	}

	profileIn := make(chan types.StudentProfile, 1)
	attemptIn := make(chan types.Attempt, 1)
	coachInput := make(chan types.Score, 32)

	// === The serving pipeline: profile -> topic-request -> question ===
	pickTopic := channels.Map(s.curriculum.Next)
	lookup := channels.Map(func(req types.TopicRequest) types.Question {
		q, err := s.bank.Find(req.Topic, req.Difficulty, req.Avoid)
		if err != nil {
			log.Printf("[lookup] miss for %s: %v", req.Topic.Name, err)
			return types.Question{ID: "no-question"}
		}
		return q
	})
	servingPipeline := channels.Pipe2(pickTopic, lookup)
	questions := servingPipeline(s.ctx, profileIn)

	// === The grading pipeline: attempt -> score ===
	// Tee2 so one branch updates the UI, the other feeds the coach.
	scoresRaw := channels.Map(s.grader.Grade)(s.ctx, attemptIn)
	toUI, toCoach := channels.Tee2(s.ctx, scoresRaw)

	// === Coach runs in background — windows of 3 scores -> recommendations ===
	go func() {
		windows := channels.Window[types.Score](3)(s.ctx, toCoach)
		updates := channels.MapCtx(s.coach.Recommend)(s.ctx, windows)
		for upd := range updates {
			s.applyCoachUpdate(studentID, upd)
		}
	}()

	// Drain coach feedback channel (kept for future expansion)
	go func() {
		for range coachInput {
		}
	}()

	sess := &session{
		studentID:  studentID,
		profile:    initialProfile(studentID),
		profileIn:  profileIn,
		questions:  questions,
		attemptIn:  attemptIn,
		scores:     toUI,
		coachInput: coachInput,
	}
	s.sessions[studentID] = sess
	return sess
}

func (s *server) applyCoachUpdate(studentID string, upd types.CoachUpdate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[studentID]
	if !ok {
		return
	}
	// Apply weak-skill updates
	weak := make(map[string]bool)
	for _, w := range sess.profile.WeakSkills {
		weak[w] = true
	}
	for _, w := range upd.AddWeak {
		weak[w] = true
	}
	for _, w := range upd.RemoveWeak {
		delete(weak, w)
	}
	sess.profile.WeakSkills = sess.profile.WeakSkills[:0]
	for w := range weak {
		sess.profile.WeakSkills = append(sess.profile.WeakSkills, w)
	}
	log.Printf("[coach] student=%s weak=%v reason=%s",
		studentID, sess.profile.WeakSkills, upd.Reason)
}

func initialProfile(id string) types.StudentProfile {
	return types.StudentProfile{
		StudentID:   id,
		WeakSkills:  []string{},
		SeenIDs:     []string{},
		TargetLevel: 3,
	}
}

// =============================================================================
// HANDLERS
// =============================================================================

func (s *server) handleNext(w http.ResponseWriter, r *http.Request) {
	studentID := r.URL.Query().Get("student")
	if studentID == "" {
		studentID = "demo"
	}
	subject := r.URL.Query().Get("subject")
	sess := s.getSession(studentID)

	s.mu.Lock()
	if subject != "" {
		sess.profile.Subject = types.Subject(subject)
	}
	prof := sess.profile
	s.mu.Unlock()

	// Push profile, pull question
	select {
	case sess.profileIn <- prof:
	case <-r.Context().Done():
		return
	case <-time.After(2 * time.Second):
		http.Error(w, "pipeline backpressure", 503)
		return
	}

	select {
	case q := <-sess.questions:
		if q.ID == "no-question" {
			http.Error(w, "no question available for this topic", 404)
			return
		}
		// Track that we showed it
		s.mu.Lock()
		sess.profile.SeenIDs = append(sess.profile.SeenIDs, q.ID)
		// Cap seen list to avoid unbounded growth in long sessions
		if len(sess.profile.SeenIDs) > 50 {
			sess.profile.SeenIDs = sess.profile.SeenIDs[len(sess.profile.SeenIDs)-50:]
		}
		s.mu.Unlock()
		writeJSON(w, q)
	case <-r.Context().Done():
		return
	case <-time.After(15 * time.Second):
		http.Error(w, "timeout generating question", 504)
	}
}

func (s *server) handleSubmit(w http.ResponseWriter, r *http.Request) {
	var att types.Attempt
	if err := json.NewDecoder(r.Body).Decode(&att); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if att.StudentID == "" {
		att.StudentID = "demo"
	}
	att.Submitted = time.Now()
	sess := s.getSession(att.StudentID)

	select {
	case sess.attemptIn <- att:
	case <-r.Context().Done():
		return
	}

	select {
	case score := <-sess.scores:
		s.bank.RecordAttempt(att, score)
		writeJSON(w, score)
	case <-r.Context().Done():
		return
	case <-time.After(5 * time.Second):
		http.Error(w, "timeout grading", 504)
	}
}

func (s *server) handleState(w http.ResponseWriter, r *http.Request) {
	studentID := r.URL.Query().Get("student")
	if studentID == "" {
		studentID = "demo"
	}
	sess := s.getSession(studentID)
	s.mu.Lock()
	defer s.mu.Unlock()
	writeJSON(w, map[string]any{
		"student_id":  sess.studentID,
		"weak_skills": sess.profile.WeakSkills,
		"seen_count":  len(sess.profile.SeenIDs),
		"subject":     sess.profile.Subject,
		"bank_size":   s.bank.Count(),
	})
}

// =============================================================================
// HELPERS
// =============================================================================

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func logMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if r.URL.Path != "/" && r.URL.Path != "/index.html" {
			log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
		}
	})
}

// Used during startup if we need to silence the unused-import warning
var _ = fmt.Sprintf
var _ = os.Getenv
