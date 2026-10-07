package panel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/linguo2625469/workbuddy2api-panel/internal/auth"
	"github.com/linguo2625469/workbuddy2api-panel/internal/pool"
	"github.com/linguo2625469/workbuddy2api-panel/internal/upstream"
)

type queueRoundTripper func(*http.Request) (*http.Response, error)

func (f queueRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAutoAllReportsTemplateFailureAndKeepsProgress(t *testing.T) {
	oldActions, oldAttempts := autoActions, claimPollAttempts
	autoActions = []autoAction{{TaskCode: "template_5", Desc: "templates", run: runTemplateUse}}
	claimPollAttempts = 1
	t.Cleanup(func() { autoActions, claimPollAttempts = oldActions, oldAttempts })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/v2/report") {
			http.Error(w, "local report fixture failure", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"tasks":[{"task_code":"template_5","target":5,"current":0,"accept_status":"accepted"}]}}`))
	}))
	defer srv.Close()

	up := upstream.New()
	up.HTTP = srv.Client()
	up.ChatBaseCN = srv.URL
	p := New(Config{Upstream: up})
	results := p.runAutoAll(context.Background(), &auth.Auth{UID: "fixture", AccessToken: "test-only"})
	if len(results) != 1 {
		t.Fatalf("results=%v, want one template action result", results)
	}
	got := results[0]
	if got["status"] != "error" {
		t.Fatalf("status=%v, want error; result=%v", got["status"], got)
	}
	if !strings.Contains(got["message"].(string), "第 1 组模板事件上报失败") {
		t.Fatalf("failure detail lost: %v", got["message"])
	}
	if got["progress_after"] != "0/5" {
		t.Fatalf("partial progress missing: %v", got["progress_after"])
	}
}

func TestGrowthQueueStopCancelsOutboundAndJoinsWorker(t *testing.T) {
	oldActions := autoActions
	autoActions = []autoAction{{TaskCode: "template_5", Desc: "templates", run: runTemplateUse}}
	t.Cleanup(func() { autoActions = oldActions })

	reportStarted := make(chan struct{}, 1)
	reportCanceled := make(chan struct{}, 1)
	transport := queueRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/v2/report") {
			reportStarted <- struct{}{}
			<-r.Context().Done()
			reportCanceled <- struct{}{}
			return nil, r.Context().Err()
		}
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/v2/activity/growth/tasks") {
			body := `{"code":0,"data":{"tasks":[{"task_code":"template_5","target":5,"current":0,"accept_status":"accepted"}]}}`
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		}
		return nil, errors.New(fmt.Sprintf("unexpected %s %s", r.Method, r.URL.Path))
	})

	parent, cancel := context.WithCancel(context.Background())
	p := New(Config{Pool: pool.New(""), Upstream: &upstream.Client{HTTP: &http.Client{Transport: transport}, ChatBaseCN: "http://fixture.invalid", BillingBaseCN: "http://fixture.invalid"}})
	p.cfg.Pool.Add(&auth.Auth{UID: "fixture", AccessToken: "test-only", RefreshToken: "test-only"})
	p.SetLifecycleContext(parent)
	started, _, _, msg := p.startGrowthQueueContext(parent, 1, true)
	if !started {
		t.Fatalf("queue did not start: %s", msg)
	}
	select {
	case <-reportStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("queue did not reach its outbound task request")
	}
	cancel()
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer stopCancel()
	if err := p.Stop(stopCtx); err != nil {
		t.Fatalf("panel stop did not join queue: %v", err)
	}
	select {
	case <-reportCanceled:
	case <-time.After(1 * time.Second):
		t.Fatal("in-flight queue request did not observe cancellation")
	}
	q := p.queue()
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.running {
		t.Fatal("queue still running after Stop returned")
	}
	for _, item := range q.items {
		if item.Status == "pending" || item.Status == "running" || item.Status == "done" {
			t.Fatalf("queue item has unsafe post-stop status: %+v", item)
		}
	}
}
