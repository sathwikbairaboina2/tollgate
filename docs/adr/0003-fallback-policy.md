# ADR 0003: Fall back only on transport errors, 429 and 5xx, and only before the first byte

Date: 2026-10-03 · Status: accepted

## Decision

- A route is an ordered target list. The gateway tries the next target when the upstream call
  fails at the transport level, returns 429, or returns any 5xx.
- Any other non-200 status (400, 401, 404, 422, ...) is passed straight through, and no other
  target is tried. The request itself is wrong, so sending it elsewhere would fail the same way and
  burn quota.
- For streams, the decision is made on the upstream status line. Once a byte has gone to the
  client, the gateway never switches providers.
- One reservation covers the whole route. It is released if every target fails or the upstream
  rejects the request.

## What I gave up

- **Mid-stream failover.** If an upstream dies halfway through a stream, the client gets a
  truncated stream. Splicing in a second provider's continuation would produce text no single model wrote.
- **Retries on the same target**, backoff and circuit breakers. Fallback makes one attempt per target.
