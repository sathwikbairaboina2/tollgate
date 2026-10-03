# ADR 0005: 402 for an exhausted budget, 429 with Retry-After for rate limits

Date: 2026-10-03 · Status: accepted

## Decision

Budget rejections return `402 Payment Required` with code `budget_exceeded`. Rate-limit rejections
return `429 Too Many Requests` with a `Retry-After` header and code `rate_limited`. Both bodies use the
OpenAI error shape `{"error":{"message","type","code"}}`.

## What I gave up

- **Bug-for-bug OpenAI compatibility.** OpenAI reports quota exhaustion as `429 insufficient_quota`.
  SDKs retry 429s automatically, and for an exhausted budget that is a retry loop that can never
  succeed. A separate status makes "stop" and "slow down" different signals. The cost is that
  clients which special-case OpenAI's code will not recognise it.
