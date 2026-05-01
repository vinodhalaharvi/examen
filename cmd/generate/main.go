// cmd/generate runs the offline question-generation pipeline.
// Composes: curriculum-seed -> problem -> (figure ∥ distractors) -> validate -> store.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"examen/internal/agents"
	"examen/internal/channels"
	"examen/internal/llm"
	"examen/internal/render"
	"examen/internal/store"
	"examen/internal/types"
)

func main() {
	var (
		bankPath = flag.String("bank", "data/bank.json", "path to question bank file")
		count    = flag.Int("count", 12, "approximate number of questions to generate")
		useReal  = flag.Bool("real", false, "use real Claude API instead of mock")
	)
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// === Setup ===
	if err := os.MkdirAll(filepath.Dir(*bankPath), 0755); err != nil {
		log.Fatal(err)
	}
	bank, err := store.New(*bankPath)
	if err != nil {
		log.Fatal(err)
	}
	client := buildClient(*useReal)
	log.Printf("[generate] using LLM client: %s", client.Name)

	// === Build agents ===
	problemAgent := agents.NewProblem(client)
	figureAgent := agents.NewFigure(client)
	distractorAgent := agents.NewDistractor(client)
	validator := agents.NewValidator(client)

	// === Compose the generation pipeline ===
	// Stage 1: seed requests from curriculum (one per topic in catalog)
	requests := seedRequests(*count)

	// Stage 2: problem generation with retry & timeout
	makeProblem := channels.MapCtx(problemAgent.Generate)

	// Stage 3: figure ∥ distractors run in PARALLEL via Tee + Zip
	assemble := func(ctx context.Context, probs <-chan types.Problem) <-chan types.Question {
		p1, p2, p3 := channels.Tee3(ctx, probs)
		figures := channels.MapCtx(figureAgent.Render)(ctx, p1)
		choices := channels.MapCtx(distractorAgent.Make)(ctx, p2)
		// p3 carries the original problem through alongside the parallel results
		return channels.Zip3Into(ctx, p3, figures, choices,
			func(p types.Problem, f types.Figure, cs []types.Choice) types.Question {
				return types.Question{
					ID:      p.ID,
					Problem: p,
					Figure:  f,
					Choices: cs,
					NextSkill: pickNextSkill(p),
				}
			})
	}

	// Stage 4: validation, then keep only the OK ones
	validate := channels.MapCtx(validator.Check)
	logVal := channels.Tap(func(v types.ValidatedQuestion) {
		log.Printf("[validator] q=%s ok=%v conf=%.2f issues=%v",
			v.Question.ID, v.OK, v.Confidence, v.Issues)
	})
	keep := channels.Filter(func(v types.ValidatedQuestion) bool { return v.OK })

	// Stage 5: persist
	persist := channels.Map(func(v types.ValidatedQuestion) types.StoredQuestion {
		stored := types.StoredQuestion{
			Question:    v.Question,
			GeneratedAt: time.Now(),
			GeneratedBy: client.Name,
			ValidatedBy: v.ValidatedBy,
		}
		// Make sure figure is rendered if we got a spec from LLM but no SVG
		if stored.Question.Figure.SVG == "" && len(stored.Question.Figure.Spec.Primitives) > 0 {
			stored.Question.Figure.SVG = render.SVG(stored.Question.Figure.Spec)
		}
		return bank.Insert(stored)
	})

	// === Wire it up ===
	source := channels.FromSlice(ctx, requests)
	problems := makeProblem(ctx, source)
	questions := assemble(ctx, problems)
	validated := validate(ctx, questions)
	tapped := logVal(ctx, validated)
	good := keep(ctx, tapped)
	stored := persist(ctx, good)

	// === Run ===
	start := time.Now()
	results := channels.Collect(ctx, stored)
	elapsed := time.Since(start)

	if err := bank.Save(); err != nil {
		log.Fatal(err)
	}

	// === Report ===
	fmt.Printf("\n=== generation complete ===\n")
	fmt.Printf("requested: %d  generated+validated: %d  rejected: %d\n",
		len(requests), len(results), len(requests)-len(results))
	fmt.Printf("elapsed: %s  bank size: %d\n", elapsed, bank.Count())
	for subj, n := range bank.CountBySubject() {
		fmt.Printf("  %s: %d\n", subj, n)
	}
	fmt.Printf("saved to %s\n", *bankPath)
}

func buildClient(useReal bool) *llm.Client {
	if useReal {
		c, err := llm.NewClaude()
		if err != nil {
			log.Printf("[warn] failed to init Claude client: %v; falling back to mock", err)
			return llm.NewMock()
		}
		return c
	}
	return llm.NewMock()
}

func seedRequests(count int) []types.TopicRequest {
	c := agents.NewCurriculum(0)
	out := make([]types.TopicRequest, 0, count)
	// Alternate subjects, cycle through topics
	profile := types.StudentProfile{StudentID: "seed"}
	for i := 0; i < count; i++ {
		if i%2 == 0 {
			profile.Subject = types.SubjectGeometry
		} else {
			profile.Subject = types.SubjectAlgebra
		}
		out = append(out, c.Next(profile))
	}
	return out
}

func pickNextSkill(p types.Problem) string {
	if len(p.Skills) > 0 {
		return p.Skills[0]
	}
	return p.Topic.Name
}
