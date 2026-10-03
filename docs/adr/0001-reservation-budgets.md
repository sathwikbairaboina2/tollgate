# ADR 0001: Budgets are enforced by worst-case reservation, not after-the-fact accounting

Date: 2026-10-03 · Status: accepted

## Context

A per-key budget that is checked only after a response arrives can always be overrun: N concurrent
requests all see "budget remaining" and all go out. The project's thesis is that the deterministic
core decides before anything leaves the building.

## Decision

- Before forwarding, the gateway **reserves** the worst-case cost of the request against the key's
  ledger and rejects with 402 if `spent + held + reservation > limit`.
- Worst case = prompt-token upper bound + `max_tokens`, priced at the **most expensive** target in the
  route, since fallback may land on any target.
- The prompt bound is the byte length of the JSON-encoded messages (and tools) plus a fixed overhead.
  Byte-level BPE tokenizers emit at most one token per UTF-8 byte, so for them this is a true upper
  bound, not a guess.
- If the client sends no `max_tokens`, or more than the key's `max_output_tokens`, the gateway
  rewrites the request to the cap. Without a cap there is no worst case.
- After the response, the reservation is settled to actual usage. If usage is missing, the gateway
  charges the full reservation (non-streaming) or the observed content bytes (streaming).
- Config validation refuses to start if a key has a USD budget and a routed model has no price:
  an unpriced model is an unbounded spend.

## What I gave up

- **Budget utilisation near the edge.** The byte bound is about 4x the real token count for
  English, so a key with little budget left gets 402s that a precise tokenizer would have let through.
- **Exact pre-flight counts.** No tiktoken or sentencepiece in the hot path: no per-model tokenizer
  tables and no CGO, at the price of a conservative estimate.
- **Strict bounds for non-byte-level tokenizers.** For some SentencePiece models, bytes are not a
  proven bound. In practice they produce fewer tokens than bytes, but nothing here proves it.
