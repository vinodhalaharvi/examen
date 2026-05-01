# Examen — multi-agent exam prep, channel-driven

A working hackathon-grade demo that turns a swarm of specialized LLM agents into
an adaptive math practice tool. Eight agents, two pipelines, no interfaces —
just generic Go channels and functional composition.

## What it does

Generates math problems (geometry + algebra) with verified answers, renders
SVG figures, picks topics adaptively based on the student's weak skills,
identifies misconceptions when answers are wrong, and updates the student's
profile via a coaching loop.

## Run it in 30 seconds

```bash
# 1. Generate a question bank using the mock LLM (no API key needed)
go run ./cmd/generate -count 12

# 2. Start the web server
go run ./cmd/serve

# 3. Open http://localhost:8080/
```

To use the real Claude API instead of the mock, set `ANTHROPIC_API_KEY` and
pass `-real`:

```bash
ANTHROPIC_API_KEY=sk-ant-... go run ./cmd/generate -real -count 30
ANTHROPIC_API_KEY=sk-ant-... go run ./cmd/serve -real
```

## Architecture

Two pipelines compose the system:

**Generation pipeline** (offline, runs via `cmd/generate`):

```
seeds → problem → ┬─ figure ──┬─ assemble → validate → filter → store
                  └─ choices ─┘
                  (parallel via Tee + Zip)
```

**Serving pipeline** (online, runs in `cmd/serve` per session):

```
profile → curriculum → lookup → question → UI
attempt → grader → score → UI
                       └→ window(3) → coach → profile update
                          (Tee branch, runs concurrently)
```

## The eight agents

| Agent | Input → Output | What it does | Model tier |
|---|---|---|---|
| Curriculum | Profile → TopicRequest | Picks topic, biased toward weak skills | rule-based |
| Problem | TopicRequest → Problem | Generates math problem with verified answer | Sonnet |
| Figure | Problem → Figure | Emits FigureSpec JSON, rendered to SVG | Sonnet |
| Distractor | Problem → []Choice | Wrong answers tagged with misconceptions | Haiku |
| Validator | Question → ValidatedQuestion | Independent check, sanity rules | Opus |
| Lookup | TopicRequest → Question | Pulls from bank avoiding seen IDs | (storage) |
| Grader | Attempt → Score | Compares answer, identifies misconception | rule-based |
| Coach | []Score → CoachUpdate | Pattern-detects weak skills from history | Haiku |

## The composition story

Every agent is a struct with one method `(In) → Out`. They become channel
agents via `channels.Map(agent.Method)`. Pipelines are just composed Maps:

```go
// Question generation (cmd/generate/main.go)
makeProblem := channels.MapCtx(problemAgent.Generate)

assemble := func(ctx, probs) <-chan Question {
    p1, p2, p3 := channels.Tee3(ctx, probs)            // split 3 ways
    figures := channels.MapCtx(figureAgent.Render)(ctx, p1)     // parallel
    choices := channels.MapCtx(distractorAgent.Make)(ctx, p2)   // parallel
    return channels.Zip3Into(ctx, p3, figures, choices, fuse)   // recombine
}

validate := channels.MapCtx(validator.Check)
keep     := channels.Filter(func(v ValidatedQuestion) bool { return v.OK })
persist  := channels.Map(toStored)

// The pipeline reads top-to-bottom like the diagram:
pipeline := channels.Pipe5(makeProblem, assemble, validate, keep, persist)
```

## Why no interfaces

Interfaces erase types at runtime. With generics and structs of function
fields (`llm.Client` is a struct, not an interface), every type stays
parametric end-to-end:

- `Agent[Problem, Figure]` is a different type than `Agent[Problem, []Choice]`
- Compiler catches modality mismatches
- No reflection, no type assertions, no `any` smuggling

The cost: no higher-kinded types means we write `Map[A,B]` once instead of
deriving it from a `Functor` typeclass. Acceptable.

## File map

```
examen/
├── cmd/
│   ├── generate/    # offline question factory
│   └── serve/       # web server
├── internal/
│   ├── channels/    # generic combinator library
│   │   ├── core.go         # Map, Filter, FlatMap, Just, Collect
│   │   ├── compose.go      # Pipe2..Pipe6, Decorate
│   │   ├── parallel.go     # Tee2/3, Merge, Zip3Into, Pool, Tap
│   │   └── resilience.go   # Result, Retry, Timeout, Logged, Window
│   ├── types/       # domain model (concrete structs)
│   ├── llm/         # mock + Claude API client
│   ├── agents/      # 7 specialist agents
│   ├── render/      # FigureSpec → SVG
│   └── store/       # JSON-backed question bank
└── web/
    └── index.html   # student-facing UI with live agent trace
```

## What's implemented vs. mocked

- **Real:** combinator library, all agents wired up, server, UI, mock LLM,
  question bank, grader, coach, curriculum
- **Stub:** Claude API client (works but untested without a real key)
- **Hackathon-quality:** JSON store (use SQLite/Postgres for production),
  validator (just checks one-correct-choice + LLM judge; production would
  add sympy)

## Where the demo lands

1. Run `cmd/generate` on screen — judges watch 12 questions get generated and
   validated in 3 seconds with parallel figure+distractor generation visible
   in the log.
2. Open the UI — agent trace pills light up sequentially during fetch,
   parallel pair lights simultaneously.
3. Submit a wrong answer — the misconception tag appears in feedback.
4. Submit 3 wrong answers — the coach fires, weak skills appear in the UI.
5. Continue — subsequent questions are biased toward the weak skill.

## What I'd build next

- SSE streaming so questions appear progressively (stem first, then figure)
- Sympy validator for algebra (subprocess, wrap as another `Agent[Problem, bool]`)
- Difficulty escalation in coach (`DifficultyDelta` is in the type but not yet
  applied)
- Real Claude integration tested end-to-end
- Persist student profiles between sessions
