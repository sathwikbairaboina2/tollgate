package main

import (
	"testing"
	"time"
)

func TestPercentile(t *testing.T) {
	var s []time.Duration
	for i := 1; i <= 100; i++ {
		s = append(s, time.Duration(i)*time.Millisecond)
	}
	if got := percentile(s, 0.50); got != 50*time.Millisecond {
		t.Errorf("p50 = %v", got)
	}
	if got := percentile(s, 0.99); got != 99*time.Millisecond {
		t.Errorf("p99 = %v", got)
	}
	if got := percentile(s[:1], 0.99); got != time.Millisecond {
		t.Errorf("single-sample p99 = %v", got)
	}
}
