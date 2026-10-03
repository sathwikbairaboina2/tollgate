#!/usr/bin/env sh
# Demo against a running compose stack (`docker compose up -d --build`).
# Prints PASS/FAIL/SKIP per step and exits non-zero on any FAIL.
# Needs only curl. Host port: ${TOLLGATE_PORT:-5450}. Restart the stack between runs: the tiny key budget is in memory.
BASE="http://localhost:${TOLLGATE_PORT:-5450}"
DEMO="${TOLLGATE_DEMO_KEY:-local-demo}"
TINY="${TOLLGATE_TINY_KEY:-local-tiny}"
RUN="$$-$(date +%s)"   # makes prompts unique per run so earlier cache entries do not hide spend
FAILED=0
H="$(mktemp)"; B="$(mktemp)"
trap 'rm -f "$H" "$B"' EXIT

pass() { echo "PASS $1"; }
fail() { echo "FAIL $1"; FAILED=1; }

# chat KEY MODEL PROMPT [MAX_TOKENS] -> sets CODE, fills $H (headers) and $B (body)
chat() {
  mt=""; [ -n "$4" ] && mt=",\"max_tokens\":$4"
  CODE="$(curl -s -m 150 -o "$B" -D "$H" -w '%{http_code}' "$BASE/v1/chat/completions" \
    -H "Authorization: Bearer $1" -H 'Content-Type: application/json' \
    -d "{\"model\":\"$2\",\"messages\":[{\"role\":\"user\",\"content\":\"$3\"}]$mt}")"
}
header() { tr -d '\r' < "$H" | grep -i "^$1:" | head -1 | cut -d: -f2- | tr -d ' '; }

# 1. health
if [ "$(curl -s -m 10 "$BASE/healthz")" = "ok" ]; then pass "healthz"; else fail "healthz"; fi

# 2. 200 then cache hit
chat "$DEMO" chat-default "demo cache $RUN"
if [ "$CODE" = "200" ] && [ "$(header X-Tollgate-Cache)" = "miss" ]; then pass "200 miss"; else fail "200 miss (code $CODE)"; fi
chat "$DEMO" chat-default "demo cache $RUN"
if [ "$CODE" = "200" ] && [ "$(header X-Tollgate-Cache)" = "hit" ]; then pass "cache hit"; else fail "cache hit (code $CODE)"; fi

# 3a. spaced requests (under the 1 rps bucket) until the tiny budget runs out: 402
got402=""
i=0
while [ $i -lt 20 ]; do
  i=$((i + 1))
  chat "$TINY" chat-default "tiny budget $RUN $i"
  [ "$CODE" = "402" ] && { got402=$i; break; }
  [ "$CODE" = "200" ] || { fail "402 sequence (unexpected $CODE on try $i)"; break; }
  sleep 1.1
done
if [ -n "$got402" ] && grep -q budget_exceeded "$B"; then pass "402 budget_exceeded after $got402 requests"; \
elif [ -z "$got402" ]; then fail "402 budget_exceeded (not reached in 20 tries)"; fi

# 3b. back-to-back requests against a fresh-enough bucket: 429 with Retry-After
sleep 3
got429=""; retry=""
for n in 1 2 3 4 5 6; do
  chat "$TINY" chat-default "tiny burst $RUN $n"
  if [ "$CODE" = "429" ]; then got429=1; retry="$(header Retry-After)"; break; fi
done
if [ -n "$got429" ] && [ -n "$retry" ]; then pass "429 rate_limited (Retry-After: ${retry}s)"; else fail "429 with Retry-After"; fi

# 4. host Ollama route (falls back to the fake upstream if Ollama is down)
if curl -s -m 5 -o /dev/null http://localhost:11434/api/tags; then
  chat "$DEMO" local-ollama "Reply with the single word: pong $RUN" 32
  if [ "$CODE" = "200" ] && grep -qi gemma4 "$B"; then pass "ollama route (model $(grep -o '"model":"[^"]*"' "$B" | head -1), provider $(header X-Tollgate-Provider))"; \
  else fail "ollama route (code $CODE, provider $(header X-Tollgate-Provider))"; fi
else
  echo "SKIP ollama (host Ollama not reachable on :11434)"
fi

# 5. metrics
M="$(curl -s -m 10 "$BASE/metrics")"
for reason in budget_exceeded rate_limited; do
  if echo "$M" | grep -q "^tollgate_rejections_total{.*reason=\"$reason\".*} [1-9]"; then pass "metrics rejections $reason"; else fail "metrics rejections $reason"; fi
done

[ "$FAILED" = "0" ] && echo "ALL PASS" || echo "SOME FAILED"
exit "$FAILED"
