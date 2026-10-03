# ADR 0007: OpenAI chat-completions is the only wire format in v0.1

Date: 2026-10-03 · Status: accepted

## Decision

Clients speak OpenAI `/v1/chat/completions`, and so must upstreams (OpenAI, Groq, Together, vLLM,
LM Studio, Ollama's `/v1`). The request body passes through as a JSON map. The gateway touches only
`model`, `max_tokens`/`max_completion_tokens` and `stream_options`.

## What I gave up

- **Native Anthropic features** (prompt caching, thinking blocks) until an adapter exists. The
  adapter is stretch work behind the same `provider` boundary.
- **Typed request validation.** Unknown fields pass through untouched. New OpenAI parameters keep
  working, but the gateway cannot reject a malformed `tools` array early.
