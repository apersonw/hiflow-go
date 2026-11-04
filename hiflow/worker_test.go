package hiflow

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkerRenewsAndCompletes(t *testing.T) {
	var renewed atomic.Int32
	var completed atomic.Bool
	srv := fakeServer(t, func(r *http.Request, body []byte) (any, *Error) {
		var req WorkLeaseRequest
		_ = json.Unmarshal(body, &req)
		if req.WorkerID != "w" || req.LockToken != "fence" {
			t.Errorf("lease identity %s", body)
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "extend-lock"):
			renewed.Add(1)
			return map[string]any{}, nil
		case strings.HasSuffix(r.URL.Path, "complete"):
			if req.Variables["approved"] != false {
				t.Errorf("variables %v", req.Variables)
			}
			completed.Store(true)
			return map[string]any{}, nil
		}
		t.Errorf("unexpected %s", r.URL.Path)
		return map[string]any{}, nil
	})
	c := newTestClient(t, srv)
	c.runJob(context.Background(), WorkerOptions{WorkerID: "w", LockSeconds: 5, Handle: func(ctx context.Context, task WorkItem) (map[string]any, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
		return map[string]any{"approved": false}, nil
	}}, WorkItem{TaskID: 7, LockToken: "fence"}, func(err error) {
		if err != nil {
			t.Error(err)
		}
	})
	if renewed.Load() == 0 || !completed.Load() {
		t.Fatalf("renewed %d completed %v", renewed.Load(), completed.Load())
	}
}
func TestWorkerCancelsHandlerOnLostLease(t *testing.T) {
	var completed atomic.Bool
	srv := fakeServer(t, func(r *http.Request, body []byte) (any, *Error) {
		if strings.HasSuffix(r.URL.Path, "extend-lock") {
			return nil, &Error{StatusCode: 409, Code: CodeConflict, Message: "lost"}
		}
		completed.Store(true)
		return map[string]any{}, nil
	})
	c := newTestClient(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var handledCancellation atomic.Bool
	c.runJob(ctx, WorkerOptions{WorkerID: "w", LockSeconds: 5, Handle: func(ctx context.Context, _ WorkItem) (map[string]any, error) {
		<-ctx.Done()
		handledCancellation.Store(true)
		return nil, ctx.Err()
	}}, WorkItem{TaskID: 7, LockToken: "stale"}, func(error) {})
	if !handledCancellation.Load() || completed.Load() {
		t.Fatal("lost lease allowed completion")
	}
}
