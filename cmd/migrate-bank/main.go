// cmd/migrate-bank reads an old JSON-backed bank (data/bank.json) and writes
// its contents into a new SQLite database (data/examen.db).
//
// Usage:
//
//	go run ./cmd/migrate-bank -from data/bank.json -to data/examen.db
//
// Run once, after pulling the SQLite migration, to preserve any questions you
// previously generated. Safe to run multiple times — Insert is idempotent.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"examen/internal/store"
	"examen/internal/types"
)

type oldFileFormat struct {
	Questions []types.StoredQuestion `json:"questions"`
	Attempts  []types.Attempt        `json:"attempts"`
	Scores    []types.Score          `json:"scores"`
}

func main() {
	from := flag.String("from", "data/bank.json", "old JSON bank path")
	to := flag.String("to", "data/examen.db", "new SQLite database path")
	flag.Parse()

	data, err := os.ReadFile(*from)
	if err != nil {
		log.Fatalf("read %s: %v", *from, err)
	}
	var old oldFileFormat
	if err := json.Unmarshal(data, &old); err != nil {
		log.Fatalf("parse %s: %v", *from, err)
	}
	log.Printf("[migrate] read %d questions, %d attempts, %d scores from %s",
		len(old.Questions), len(old.Attempts), len(old.Scores), *from)

	if err := os.MkdirAll(filepath.Dir(*to), 0755); err != nil {
		log.Fatal(err)
	}
	bank, err := store.New(*to)
	if err != nil {
		log.Fatalf("open %s: %v", *to, err)
	}
	defer bank.Close()

	// Migrate questions. Insert is upsert, so re-running is safe.
	for _, q := range old.Questions {
		bank.Insert(q)
	}
	log.Printf("[migrate] wrote %d questions into %s", len(old.Questions), *to)

	// Migrate attempt+score pairs. The two slices were stored independently
	// in the JSON file; pair them by index, which is the order they were
	// originally appended in.
	pairs := len(old.Attempts)
	if len(old.Scores) < pairs {
		pairs = len(old.Scores)
	}
	for i := 0; i < pairs; i++ {
		bank.RecordAttempt(old.Attempts[i], old.Scores[i])
	}
	if pairs > 0 {
		log.Printf("[migrate] wrote %d attempt/score pairs into %s", pairs, *to)
	}

	fmt.Printf("\n=== migration complete ===\n")
	fmt.Printf("source: %s\n", *from)
	fmt.Printf("target: %s\n", *to)
	fmt.Printf("bank size: %d questions\n", bank.Count())
	for subj, n := range bank.CountBySubject() {
		fmt.Printf("  %s: %d\n", subj, n)
	}
	fmt.Printf("\nNext: run `go run ./cmd/serve` to use the new database.\n")
	fmt.Printf("You can keep %s as a backup or delete it once verified.\n", *from)
}
