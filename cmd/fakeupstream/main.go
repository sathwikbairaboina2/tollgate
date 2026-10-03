// Command fakeupstream serves a deterministic OpenAI-compatible chat API for demos and benchmarks.
package main

import (
	"flag"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/sathwikbairaboina2/tollgate/internal/fakeupstream"
)

func main() {
	addr := flag.String("addr", ":9000", "listen address")
	delay := flag.Duration("delay", 0, "artificial latency per request")
	flag.Parse()
	slog.Info("fake upstream listening", "addr", *addr, "delay", delay.String())
	srv := &http.Server{Addr: *addr, Handler: fakeupstream.Handler(*delay), ReadHeaderTimeout: 10 * time.Second}
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("fake upstream exited", "err", err)
		os.Exit(1)
	}
}
