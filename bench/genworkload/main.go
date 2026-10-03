// Command genworkload writes a deterministic synthetic chat workload. Prompts are drawn from a
// fixed pool with a Zipf distribution, the way FAQ-style support traffic repeats. The output is
// committed so every benchmark run replays the same requests.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
)

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type record struct {
	Model     string    `json:"model"`
	Messages  []message `json:"messages"`
	MaxTokens int       `json:"max_tokens"`
}

var topics = []string{
	"resetting a password", "changing the billing email", "exporting invoices as CSV",
	"enabling two-factor authentication", "rotating an API key", "upgrading to the team plan",
	"cancelling a subscription", "adding a teammate", "removing a teammate", "configuring SSO",
	"webhook retries", "rate limit errors", "deleting an account", "changing the timezone",
	"downloading usage reports", "connecting Slack", "restoring a deleted project",
	"setting a spending cap", "moving a project between workspaces", "verifying a domain",
}

var templates = []string{
	"How do I handle %s?",
	"What are the steps for %s?",
	"I'm stuck on %s. Can you help?",
	"Is there documentation about %s?",
	"Explain %s in two sentences.",
	"What usually goes wrong with %s?",
}

func main() {
	n := flag.Int("n", 1000, "number of requests")
	seed := flag.Int64("seed", 42, "PRNG seed")
	out := flag.String("out", "bench/workload.jsonl", "output path")
	flag.Parse()

	var pool []string
	for _, t := range templates {
		for _, topic := range topics {
			pool = append(pool, fmt.Sprintf(t, topic))
		}
	}
	r := rand.New(rand.NewSource(*seed))
	zipf := rand.NewZipf(r, 1.2, 2, uint64(len(pool)-1))

	f, err := os.Create(*out)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	defer w.Flush()
	enc := json.NewEncoder(w)
	for range *n {
		rec := record{
			Model: "chat-default",
			Messages: []message{
				{Role: "system", Content: "You are a concise support assistant for a SaaS product."},
				{Role: "user", Content: pool[zipf.Uint64()]},
			},
			MaxTokens: 256,
		}
		if err := enc.Encode(rec); err != nil {
			log.Fatal(err)
		}
	}
}
