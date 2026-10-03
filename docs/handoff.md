# Handoff log

## 2026-10-03 - Claude (Sonnet builder) - branch `main`

**What changed.** Implemented Tasks 1-21 of `docs/superpowers/plans/2026-10-03-tollgate.md` (v0.1). Nothing is committed (commit authorization is pending); all files are untracked on `main`, task boundaries are in `.superpowers/sdd/2026-10-03-tollgate/progress.md`. Nothing was pushed.

- Toolchain: `scripts/go.ps1`, `scripts/go.sh`, `Makefile` (Go runs only in `golang:1.25`).
- Packages: `internal/config`, `ratelimit`, `budget`, `cache`, `provider`, `telemetry`, `gateway` (auth, rate limit, parse, cap, cache, reserve, fallback forward, JSON and SSE relay, settle), `fakeupstream`.
- Binaries: `cmd/tollgate`, `cmd/fakeupstream`. `Dockerfile` ships both.
- Demo: `docker-compose.yml` (gateway :8787, fake upstream, Prometheus :9090, Jaeger :16686, `bench` profile) with `deploy/`.
- Bench: `bench/genworkload`, `bench/workload.jsonl` (1000 lines, stable hash), `bench/` harness, `bench/results/latest.json`.
- Docs/CI: `README.md` (numbers from `latest.json`), `.github/workflows/ci.yml`, `docs/DEVDOCS.md` results link.

**Measured (compose bench run, `bench/results/latest.json`).** 1000 requests x 5 rounds, 0s upstream delay, 24 CPUs, go1.25.14: direct p50 0.141 / p99 0.321 ms, through Tollgate p50 0.270 / p99 0.749 ms, added p50 0.129 / p99 0.428 ms. Cache 897/1000 hits (89.7%), cost $0.107102 -> $0.009152 (91.5% saved). Synthetic Zipf workload, not production traffic.

**Verified.** `go test -race ./...` all packages ok; `go vet ./...` clean; `gofmt -l .` empty; `docker build -t tollgate:dev .` ok; container smoke (`ok`, 401, 502 `upstream_unavailable`); compose demo (200, cache `hit`, `local-tiny` 200,200,200 then 429s, Prometheus result, Jaeger lists `tollgate`).

**Deviations from the plan.**
- OpenTelemetry modules pinned to v1.43.0 (latest, v1.47.0, needs Go 1.26 and cannot build in `golang:1.25`). `go.mod` directive became `go 1.25.0`.
- `scripts/go.ps1` and `Makefile` written with the file tool (shell heredocs collapsed `\\`).

**What is left (stretch, from the spec).** Anthropic adapter, semantic cache, Redis-backed budget store, budget reset periods, Helm chart, streaming cache replay, tool-call-aware token counting, admin API, per-route rate limits. Also: commit the work once authorized (one commit per task, subjects are in the plan), and decide whether `.superpowers/` should be gitignored.

**Not run.** The `local-llama` route against a real host Ollama (tests and demo use fakes only). The 402 path on `local-tiny` in the compose demo was not hit before 429 took over (402 is covered by `TestBudget_*`). No Jaeger UI visual check, only the services API.

**How to verify.**
```powershell
powershell -NoProfile -File scripts/go.ps1 test -race ./...
powershell -NoProfile -File scripts/go.ps1 vet ./...
docker compose up -d --build   # then curl :8787, see :9090 and :16686
docker compose --profile bench run --rm bench
docker compose down
```

## 2026-10-03 - Claude (Opus planner/reviewer) - branch `main`

**Review.** Re-ran `scripts/go.ps1 test -race -count=1 ./...` (all 9 tested packages ok) and `vet` (clean). Reviewed the gateway pipeline against the spec: no stubs remain, and fallback, release and settle paths match ADRs 0001 and 0003. No fix round was needed. Added `.superpowers/` to `.gitignore`, and put the measured bench results into `docs/DEVDOCS.md`.

**Blocked.** Committing was denied by the auto-mode permission classifier on the first planning-docs commit, so all work is still uncommitted. The user must authorize commits; the planned one-per-task subjects are in the plan.
