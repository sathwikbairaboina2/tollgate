# ADR 0004: Exact-match cache for non-streaming requests only, behind an interface

Date: 2026-10-03 · Status: accepted

## Decision

- Cache key = SHA-256 of (route model name, canonical JSON of the request body without `stream`,
  `stream_options` and `user`). Go's `encoding/json` sorts map keys, so field order does not matter.
- The key is computed **after** the `max_tokens` cap is applied, so the cap is part of the key.
- Only 200 responses to non-streaming requests are stored, in an in-memory LRU with TTL.
- Cache hits spend no budget but still consume a rate-limit token. The limiter protects the
  gateway as well as the wallet.
- `cache.Cache` is a two-method interface (`Get`, `Set`), so a semantic (embedding) cache can be
  added later without touching the handler.

## What I gave up

- **Semantic hits.** "How do I reset my password?" and "how do i reset my password" miss each other.
- **Streaming hits.** Streaming requests always go upstream. Replaying cached SSE is stretch work.
- **Sampling semantics.** A cached answer to a `temperature: 1` request is returned verbatim. Callers
  who want fresh samples must turn the cache off or vary the request.
