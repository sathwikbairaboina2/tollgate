package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/sathwikbairaboina2/tollgate/internal/budget"
	"github.com/sathwikbairaboina2/tollgate/internal/cache"
)

// handleChat is the request pipeline. Every limit is checked before any byte goes upstream.
func (g *Gateway) handleChat(w http.ResponseWriter, r *http.Request) {
	ctx, span := g.tracer.Start(r.Context(), "chat", trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()
	span.SetAttributes(attribute.String(attrOperation, "chat"))

	ks := g.authenticate(r)
	if ks == nil {
		writeError(w, http.StatusUnauthorized, "invalid_api_key", "missing or unknown API key")
		return
	}
	span.SetAttributes(attribute.String(attrKeyID, ks.id))
	if !g.allow(w, ks) {
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "could not read request body")
		return
	}
	req, err := parseRequest(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	targets, ok := g.routes[req.Model]
	if !ok {
		writeError(w, http.StatusNotFound, "model_not_found", fmt.Sprintf("no route for model %q", req.Model))
		return
	}
	span.SetName("chat " + req.Model)
	req.capMaxTokens(ks.maxOut)
	span.SetAttributes(attribute.String(attrRequestModel, req.Model), attribute.Int(attrRequestMaxTokens, req.MaxTokens))

	cacheKey := ""
	if g.cache != nil && !req.Stream {
		cacheKey = cache.Key(req.Model, req.body)
		if g.serveCached(w, span, ks, req, cacheKey) {
			return
		}
	}
	promptBound := promptTokenBound(req.body)
	res, ok := g.reserve(w, ks, targets, promptBound, int64(req.MaxTokens))
	if !ok {
		return
	}
	g.forward(ctx, w, span, ks, req, targets, res, promptBound, cacheKey)
}

func (g *Gateway) authenticate(r *http.Request) *keyState {
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || tok == "" {
		return nil
	}
	return g.keys[tok]
}

// allow takes a rate-limit token or answers 429 with Retry-After. It runs before the body is
// read, so a flood of requests costs no parsing.
func (g *Gateway) allow(w http.ResponseWriter, ks *keyState) bool {
	if ks.bucket == nil {
		return true
	}
	ok, wait := ks.bucket.Allow()
	if ok {
		return true
	}
	w.Header().Set("Retry-After", retryAfterSeconds(wait))
	g.metrics.Rejections.WithLabelValues(ks.id, "rate_limited").Inc()
	writeError(w, http.StatusTooManyRequests, "rate_limited", "rate limit exceeded")
	return false
}

// serveCached answers from the exact-match cache. Hits spend no budget (ADR 0004).
func (g *Gateway) serveCached(w http.ResponseWriter, span trace.Span, ks *keyState, req *chatRequest, key string) bool {
	body, ok := g.cache.Get(key)
	span.SetAttributes(attribute.Bool(attrCacheHit, ok))
	if !ok {
		return false
	}
	g.metrics.CacheHits.WithLabelValues(ks.id).Inc()
	g.metrics.Requests.WithLabelValues(ks.id, req.Model, "cache_hit").Inc()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Tollgate-Cache", "hit")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
	return true
}

// reserve holds the worst-case cost (prompt bound + output cap, priced at the most expensive
// target in the route) or answers 402. See docs/adr/0001-reservation-budgets.md.
func (g *Gateway) reserve(w http.ResponseWriter, ks *keyState, targets []target, promptBound, maxOut int64) (*budget.Reservation, bool) {
	worst := 0.0
	for _, t := range targets {
		worst = max(worst, budget.Cost(t.price, promptBound, maxOut))
	}
	res, err := ks.ledger.Reserve(promptBound+maxOut, worst)
	if err != nil {
		g.metrics.Rejections.WithLabelValues(ks.id, "budget_exceeded").Inc()
		writeError(w, http.StatusPaymentRequired, "budget_exceeded", "request would exceed the key's budget")
		return nil, false
	}
	return res, true
}

// forward tries each target in order. Transport errors, 429 and 5xx move on to the next target;
// any other non-200 is the client's problem and passes through (ADR 0003).
func (g *Gateway) forward(ctx context.Context, w http.ResponseWriter, span trace.Span, ks *keyState, req *chatRequest, targets []target, res *budget.Reservation, promptBound int64, cacheKey string) {
	for i, t := range targets {
		start := time.Now()
		resp, err := t.provider.Do(ctx, req.bodyFor(t.model))
		g.metrics.UpstreamLatency.WithLabelValues(t.provider.Name).Observe(time.Since(start).Seconds())
		if ctx.Err() != nil { // client went away; nothing left to answer
			if resp != nil {
				drainClose(resp)
			}
			res.Release()
			return
		}
		if reason := retryReason(resp, err); reason != "" {
			if resp != nil {
				drainClose(resp)
			}
			g.metrics.Fallbacks.WithLabelValues(t.provider.Name, reason).Inc()
			span.AddEvent("upstream_failed", trace.WithAttributes(
				attribute.String("tollgate.provider", t.provider.Name),
				attribute.String("tollgate.reason", reason),
			))
			continue
		}
		defer resp.Body.Close()
		span.SetAttributes(attribute.String(attrProviderName, t.provider.Name), attribute.Int(attrAttempts, i+1))
		w.Header().Set("X-Tollgate-Provider", t.provider.Name)
		w.Header().Set("X-Tollgate-Attempts", strconv.Itoa(i+1))
		if resp.StatusCode != http.StatusOK {
			res.Release()
			g.metrics.Requests.WithLabelValues(ks.id, req.Model, "upstream_rejected").Inc()
			g.passThrough(w, resp)
			return
		}
		g.relay(w, span, ks, req, t, resp, res, promptBound, cacheKey)
		return
	}
	res.Release()
	g.metrics.Requests.WithLabelValues(ks.id, req.Model, "upstream_unavailable").Inc()
	span.SetStatus(codes.Error, "all upstream targets failed")
	writeError(w, http.StatusBadGateway, "upstream_unavailable", "all upstream targets failed")
}

// retryReason names why an attempt should fall through to the next target, or "" if it should not.
func retryReason(resp *http.Response, err error) string {
	switch {
	case err != nil:
		return "transport_error"
	case resp.StatusCode == http.StatusTooManyRequests:
		return "rate_limited"
	case resp.StatusCode >= 500:
		return "server_error"
	}
	return ""
}

// drainClose discards a small amount of the body so the connection can be reused, then closes it.
func drainClose(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	_ = resp.Body.Close()
}

func (g *Gateway) passThrough(w http.ResponseWriter, resp *http.Response) {
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, io.LimitReader(resp.Body, maxUpstreamBytes))
}

// relay writes a successful upstream response to the client.
func (g *Gateway) relay(w http.ResponseWriter, span trace.Span, ks *keyState, req *chatRequest, t target, resp *http.Response, res *budget.Reservation, promptBound int64, cacheKey string) {
	if req.Stream {
		g.relayStream(w, span, ks, req, t, resp, res, promptBound)
		return
	}
	g.relayJSON(w, span, ks, req, t, resp, res, promptBound, cacheKey)
}

type upstreamUsage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
}

type completion struct {
	Model   string `json:"model"`
	Choices []struct {
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *upstreamUsage `json:"usage"`
}

func (g *Gateway) relayJSON(w http.ResponseWriter, span trace.Span, ks *keyState, req *chatRequest, t target, resp *http.Response, res *budget.Reservation, promptBound int64, cacheKey string) {
	worstIn, worstOut := promptBound, int64(req.MaxTokens)
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxUpstreamBytes))
	if err != nil {
		// The upstream may already have billed us: charge the full reservation.
		g.settle(span, ks, req, t, res, worstIn, worstOut, "", nil)
		writeError(w, http.StatusBadGateway, "upstream_unavailable", "upstream response was cut off")
		return
	}
	var c completion
	_ = json.Unmarshal(body, &c)
	in, out := worstIn, worstOut // no usage reported: charge the worst case
	if c.Usage != nil {
		in, out = c.Usage.PromptTokens, c.Usage.CompletionTokens
	}
	var finish []string
	for _, ch := range c.Choices {
		if ch.FinishReason != nil && *ch.FinishReason != "" {
			finish = append(finish, *ch.FinishReason)
		}
	}
	g.settle(span, ks, req, t, res, in, out, c.Model, finish)
	if cacheKey != "" {
		g.cache.Set(cacheKey, body)
		w.Header().Set("X-Tollgate-Cache", "miss")
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// settle converts the reservation into actual spend and records metrics and span attributes.
func (g *Gateway) settle(span trace.Span, ks *keyState, req *chatRequest, t target, res *budget.Reservation, in, out int64, respModel string, finish []string) {
	cost := budget.Cost(t.price, in, out)
	res.Settle(in+out, cost)
	g.metrics.Tokens.WithLabelValues(ks.id, t.model, "input").Add(float64(in))
	g.metrics.Tokens.WithLabelValues(ks.id, t.model, "output").Add(float64(out))
	g.metrics.CostUSD.WithLabelValues(ks.id, t.model).Add(cost)
	g.metrics.Requests.WithLabelValues(ks.id, req.Model, "ok").Inc()
	span.SetAttributes(
		attribute.Int64(attrUsageInput, in),
		attribute.Int64(attrUsageOutput, out),
		attribute.Float64(attrCostUSD, cost),
	)
	if respModel != "" {
		span.SetAttributes(attribute.String(attrResponseModel, respModel))
	}
	if len(finish) > 0 {
		span.SetAttributes(attribute.StringSlice(attrFinishReasons, finish))
	}
}

// retryAfterSeconds rounds a wait up to whole seconds, minimum 1.
func retryAfterSeconds(d time.Duration) string {
	s := int((d + time.Second - 1) / time.Second)
	if s < 1 {
		s = 1
	}
	return strconv.Itoa(s)
}
