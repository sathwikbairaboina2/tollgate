# ADR 0006: GenAI span attribute keys are string constants, not the semconv Go package

Date: 2026-10-03 · Status: accepted

## Decision

Spans follow the OpenTelemetry GenAI semantic conventions (`gen_ai.operation.name`,
`gen_ai.provider.name`, `gen_ai.request.model`, `gen_ai.request.max_tokens`, `gen_ai.response.model`,
`gen_ai.usage.input_tokens`, `gen_ai.usage.output_tokens`, `gen_ai.response.finish_reasons`). The keys
are declared as constants in `internal/gateway/attrs.go`, and Tollgate-specific attributes use the
`tollgate.` prefix. Export uses OTLP/HTTP when `OTEL_EXPORTER_OTLP_ENDPOINT` is set and is a no-op otherwise.

## What I gave up

- **Compile-time tracking of convention changes.** The GenAI conventions are still in Development
  status and have renamed keys before (`gen_ai.system` became `gen_ai.provider.name`). Pinning a
  versioned `semconv/vX` package would churn imports. Hand-written keys can drift silently, so a span
  test pins the exact keys.
