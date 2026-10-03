# ADR 0009: Host ports 5450-5452 for the demo stack

Status: accepted (2026-10-04)

## Context

The development machine runs many sibling stacks, and the default ports for this demo (8787, 9090, 16686)
collide with other tools. Only host ports 5450-5459 are reserved for this project.

## Decision

- Compose publishes the gateway on 5450, Prometheus on 5451 and the Jaeger UI on 5452.
- Each is overridable with `TOLLGATE_PORT`, `TOLLGATE_PROM_PORT`, `TOLLGATE_JAEGER_PORT`.
- `make run` publishes the gateway on `${TOLLGATE_PORT:-5450}`.
- The gateway still listens on `:8787` inside its container; the product default in config and code is unchanged.
- Observability images are pinned instead of `:latest`: `prom/prometheus:v3.15.0` and
  `jaegertracing/all-in-one:1.76.0` (the versions found in the local image cache).

## Consequences

- README curl examples use `localhost:5450`.
- Config files and container-internal addresses (`:8787`, `jaeger:4318`, `fake-upstream:9000`) are unchanged.
- Upgrading Prometheus or Jaeger is now an explicit edit of `docker-compose.yml`.
