// cmd/serve runs the live student-facing web server.
//
// Composes two pipelines:
//   1. Serving:   StudentProfile -> Question (curriculum -> lookup)
//   2. Grading:   Attempt -> Score (grader, with coach observing via Tee)
//
// The HTTP handlers push values into channels and read results out.
//
// Authentication:
//
// If GOOGLE_OAUTH_CLIENT_ID, GOOGLE_OAUTH_CLIENT_SECRET, GOOGLE_OAUTH_REDIRECT_URL
// and EXAMEN_COOKIE_KEY are all set, the server requires Google sign-in for
// all /api/* endpoints. The student id used by every agent is the verified
// email address from Google.
//
// If those env vars are missing, the server runs in anonymous mode: the
// student id comes from a `student` query parameter (legacy behavior). This
// keeps the demo runnable without Google credentials.
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
	"examen/internal/auth"
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

	// === Auth setup (optional) ===
	authCfg, authErr := auth.LoadConfig()
	if authErr != nil {
		log.Printf("[serve] auth disabled (anonymous mode): %v", authErr)
		log.Printf("[serve] to enable Google sign-in, set GOOGLE_OAUTH_CLIENT_ID, GOOGLE_OAUTH_CLIENT_SECRET, GOOGLE_OAUTH_REDIRECT_URL, EXAMEN_COOKIE_KEY")
	} else {
		log.Printf("[serve] auth enabled: Google OAuth, redirect=%s",
			authCfg.OAuth.RedirectURL)
	}

	// === Build agents ===
	curriculum := agents.NewCurriculum(time.Now().UnixNano())
	grader := agents.NewGrader(bank.Get)
	coach := agents.NewCoach(client)

	// === Build the server ===
	srv := newServer(ctx, bank, curriculum, grader, coach, authCfg)

	mux := http.NewServeMux()

	// Auth routes — only registered when auth is configured
	if authCfg != nil {
		mux.HandleFunc("/auth/login", authCfg.Login)
		mux.HandleFunc("/auth/callback", authCfg.Callback)
		mux.HandleFunc("/auth/logout", authCfg.Logout)
		mux.HandleFunc("/auth/me", authCfg.Me)
		mux.HandleFunc("/login", serveLoginPage(*webDir))
	}

	// API routes
	apiMux := http.NewServeMux()
	apiMux.HandleFunc("/api/question/next", srv.handleNext)
	apiMux.HandleFunc("/api/answer/submit", srv.handleSubmit)
	apiMux.HandleFunc("/api/session/state", srv.handleState)

	if authCfg != nil {
		mux.Handle("/api/", authCfg.Require(apiMux))
	} else {
		mux.Handle("/api/", apiMux)
	}

	// Static files — root path. With auth enabled, redirect unauthenticated
	// browsers to the login page before they even see the app shell.
	staticHandler := http.FileServer(http.Dir(*webDir))
	if authCfg != nil {
		mux.Handle("/", redirectIfAnon(authCfg, staticHandler))
	} else {
		mux.Handle("/", staticHandler)
	}

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

// redirectIfAnon redirects unauthenticated browsers to /login for the app
// shell. The login page itself is exempt (otherwise it would be a redirect
// loop). Static assets like /favicon.ico and /static/* pass through so the
// login page can include CSS or fonts.
func redirectIfAnon(authCfg *auth.Config, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Allow the login page and obvious public assets through.
		if r.URL.Path == "/login" || r.URL.Path == "/login.html" ||
			r.URL.Path == "/favicon.ico" {
			next.ServeHTTP(w, r)
			return
		}
		if _, ok := authCfg.SessionFrom(r); ok {
			next.ServeHTTP(w, r)
			return
		}
		http.Redirect(w, r, "/login", http.StatusFound)
	})
}

// serveLoginPage returns a handler that serves web/login.html.
func serveLoginPage(webDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, webDir+"/login.html")
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
	auth       *auth.Config // nil in anonymous mode

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
	g *agents.Grader, coach *agents.Coach, authCfg *auth.Config,
) *server {
	return &server{
		ctx: ctx, bank: bank,
		curriculum: c, grader: g, coach: coach, auth: authCfg,
		sessions: make(map[string]*session),
	}
}

// studentIDFor pulls the canonical student id for the request. With auth
// configured, this is the verified email from the session cookie; without
// auth, it falls back to the legacy ?student=... query param.
func (s *server) studentIDFor(r *http.Request) string {
	if s.auth != nil {
		if sess, ok := s.auth.SessionFrom(r); ok {
			return sess.StudentID
		}
		// Auth required but no session — handlers behind auth.Require will
		// have already rejected. As a safety net return empty so the caller
		// can 401.
		return ""
	}
	if id := r.URL.Query().Get("student"); id != "" {
		return id
	}
	return "demo"
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
	studentID := s.studentIDFor(r)
	if studentID == "" {
		http.Error(w, "not authenticated", http.StatusUnauthorized)
		return
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
	// Force the canonical student id from the session/query — never trust
	// the body's student_id, even if the client sends it.
	studentID := s.studentIDFor(r)
	if studentID == "" {
		http.Error(w, "not authenticated", http.StatusUnauthorized)
		return
	}
	att.StudentID = studentID
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
	studentID := s.studentIDFor(r)
	if studentID == "" {
		http.Error(w, "not authenticated", http.StatusUnauthorized)
		return
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
