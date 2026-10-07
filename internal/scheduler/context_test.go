package scheduler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/linguo2625469/workbuddy2api-panel/internal/auth"
	"github.com/linguo2625469/workbuddy2api-panel/internal/pool"
	"github.com/linguo2625469/workbuddy2api-panel/internal/upstream"
)

type contextRoundTripper func(*http.Request) (*http.Response, error)

func (f contextRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRunActivityContextCancelsRequestAndStopsNextAccount(t *testing.T) {
	fastActivity(t)
	started := make(chan struct{}, 1)
	requestCanceled := make(chan struct{}, 1)
	var calls atomic.Int32
	client := &http.Client{Transport: contextRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		started <- struct{}{}
		select {
		case <-r.Context().Done():
			requestCanceled <- struct{}{}
			return nil, errors.New("fixture request canceled")
		case <-time.After(5 * time.Second):
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"code":0,"msg":"OK"}`)), Request: r}, nil
		}
	})}

	p := pool.New("")
	p.Add(&auth.Auth{UID: "first", AccessToken: "at", RefreshToken: "rt", ExpiresAt: 9999999999})
	p.Add(&auth.Auth{UID: "second", AccessToken: "at", RefreshToken: "rt", ExpiresAt: 9999999999})
	s := New(Config{Pool: p, Upstream: &upstream.Client{HTTP: client, ChatBaseCN: "http://fixture.invalid", BillingBaseCN: "http://fixture.invalid"}})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); s.runActivity(ctx) }()

	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("outbound activity request did not start")
	}
	cancel()
	select {
	case <-requestCanceled:
	case <-time.After(3 * time.Second):
		t.Fatal("RoundTripper did not observe request cancellation")
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("scheduler activity did not return after cancellation")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("outbound requests=%d, want exactly one before cancellation", got)
	}
}

func TestBalanceRefreshCancellationJoinsSupervisorAndWorker(t *testing.T) {
	oldMin, oldMax := balanceColdStartMin, balanceColdStartMax
	balanceColdStartMin, balanceColdStartMax = 0, 0
	t.Cleanup(func() { balanceColdStartMin, balanceColdStartMax = oldMin, oldMax })
	started := make(chan struct{}, 1)
	requestCanceled := make(chan struct{}, 1)
	client := &http.Client{Transport: contextRoundTripper(func(r *http.Request) (*http.Response, error) {
		started <- struct{}{}
		<-r.Context().Done()
		requestCanceled <- struct{}{}
		return nil, r.Context().Err()
	})}
	p := pool.New("")
	p.Add(&auth.Auth{UID: "balance", AccessToken: "at", RefreshToken: "rt", ExpiresAt: 9999999999})
	s := New(Config{Pool: p, Upstream: &upstream.Client{HTTP: client, ChatBaseCN: "http://fixture.invalid", BillingBaseCN: "http://fixture.invalid"}})
	ctx, cancel := context.WithCancel(context.Background())
	s.StartBalanceRefresh(ctx, time.Hour)
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		cancel()
		t.Fatal("balance worker did not start its outbound request")
	}
	cancel()
	select {
	case <-requestCanceled:
	case <-time.After(3 * time.Second):
		t.Fatal("balance request did not observe cancellation")
	}
	joinCtx, joinCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer joinCancel()
	if err := s.WaitContext(joinCtx); err != nil {
		t.Fatalf("scheduler workers did not join: %v", err)
	}
	s.balanceWorkersMu.Lock()
	defer s.balanceWorkersMu.Unlock()
	if len(s.balanceWorkers) != 0 {
		t.Fatalf("balance workers remain after WaitContext: %d", len(s.balanceWorkers))
	}
}
