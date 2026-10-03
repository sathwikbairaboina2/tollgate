# ADR 0002: Ledgers, rate-limit buckets and the cache live in process memory

Date: 2026-10-03 · Status: accepted

## Context

v0.1 must be demoable as one binary with no dependencies. A shared store (Redis, Postgres) adds an
operational dependency and a network hop to every request.

## Decision

All mutable state (per-key ledger, token bucket, LRU cache) lives in memory behind small types
(`budget.Ledger`, `ratelimit.Bucket`, `cache.Cache`), guarded by mutexes.

## What I gave up

- **Horizontal scaling.** Each replica enforces the full budget on its own, so N replicas allow N
  times the spend. Running more than one replica is unsupported until a shared store exists.
- **Durability.** A restart resets spend to zero. Budgets also have no reset period: the period is
  the lifetime of the process.
- Both are stretch work: a Redis-backed ledger with an atomic Lua reserve/settle script, plus
  calendar reset periods.
