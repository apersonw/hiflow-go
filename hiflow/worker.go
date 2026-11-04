package hiflow

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

type WorkItem struct {
	TaskID      int64          `json:"task_id"`
	InstanceID  int64          `json:"instance_id"`
	Topic       string         `json:"topic"`
	LockToken   string         `json:"lock_token"`
	LockedUntil *time.Time     `json:"locked_until"`
	Attempt     int            `json:"attempt"`
	Remaining   int            `json:"remaining"`
	Params      map[string]any `json:"params"`
	Variables   map[string]any `json:"variables"`
}
type FetchWorkRequest struct {
	WorkerID    string   `json:"worker_id"`
	Topics      []string `json:"topics"`
	MaxTasks    int      `json:"max_tasks"`
	LockSeconds int      `json:"lock_seconds"`
	WaitSeconds int      `json:"wait_seconds"`
}
type WorkLeaseRequest struct {
	WorkerID     string         `json:"worker_id"`
	LockToken    string         `json:"lock_token"`
	LockSeconds  int            `json:"lock_seconds,omitempty"`
	Variables    map[string]any `json:"variables,omitempty"`
	ErrorCode    string         `json:"error_code,omitempty"`
	ErrorMessage string         `json:"error_message,omitempty"`
}

func (c *Client) FetchWork(ctx context.Context, req *FetchWorkRequest) ([]WorkItem, error) {
	var result []WorkItem
	err := c.post(ctx, "/open/v1/worker/fetch-and-lock", req, &result)
	return result, err
}
func (c *Client) SettleWork(ctx context.Context, id int64, action string, req *WorkLeaseRequest) error {
	switch action {
	case "complete", "fail", "extend-lock", "bpmn-error":
	default:
		return fmt.Errorf("hiflow: invalid worker action %q", action)
	}
	return c.post(ctx, fmt.Sprintf("/open/v1/worker/tasks/%d/%s", id, action), req, nil)
}

type BPMNError struct{ Code, Message string }

func (e *BPMNError) Error() string { return e.Code + ": " + e.Message }

type WorkerOptions struct {
	WorkerID    string
	Topics      []string
	Concurrency int
	LockSeconds int
	Handle      func(context.Context, WorkItem) (map[string]any, error)
	OnError     func(error)
}

// RunWorker blocks until cancellation and all handlers have returned. Handlers
// must observe context cancellation and make external effects idempotent by TaskID.
func (c *Client) RunWorker(ctx context.Context, opts WorkerOptions) error {
	if opts.WorkerID == "" || len(opts.Topics) == 0 || opts.Handle == nil {
		return errors.New("hiflow: worker ID, topics and handler are required")
	}
	if opts.Concurrency == 0 {
		opts.Concurrency = 1
	}
	if opts.Concurrency < 1 || opts.Concurrency > 100 {
		return errors.New("hiflow: concurrency must be 1..100")
	}
	if opts.LockSeconds == 0 {
		opts.LockSeconds = 60
	}
	if opts.LockSeconds < 5 || opts.LockSeconds > 3600 {
		return errors.New("hiflow: lock seconds must be 5..3600")
	}
	report := func(err error) {
		if err != nil && opts.OnError != nil {
			opts.OnError(err)
		}
	}
	var wg sync.WaitGroup
	for i := 0; i < opts.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				jobs, err := c.FetchWork(ctx, &FetchWorkRequest{WorkerID: opts.WorkerID, Topics: opts.Topics, MaxTasks: 1, LockSeconds: opts.LockSeconds, WaitSeconds: 20})
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					report(err)
					t := time.NewTimer(time.Second)
					select {
					case <-ctx.Done():
						t.Stop()
						return
					case <-t.C:
					}
					continue
				}
				for _, job := range jobs {
					c.runJob(ctx, opts, job, report)
				}
			}
		}()
	}
	wg.Wait()
	return ctx.Err()
}
func (c *Client) runJob(ctx context.Context, opts WorkerOptions, job WorkItem, report func(error)) {
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	lease := &WorkLeaseRequest{WorkerID: opts.WorkerID, LockToken: job.LockToken, LockSeconds: opts.LockSeconds}
	done := make(chan struct{})
	renewed := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(time.Duration(opts.LockSeconds) * time.Second / 3)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				renewed <- nil
				return
			case <-ctx.Done():
				renewed <- ctx.Err()
				cancel()
				return
			case <-ticker.C:
				if err := c.SettleWork(workCtx, job.TaskID, "extend-lock", lease); err != nil {
					renewed <- err
					cancel()
					return
				}
			}
		}
	}()
	var variables map[string]any
	var err error
	func() {
		defer func() {
			if recover() != nil {
				err = errors.New("worker handler panicked")
			}
		}()
		variables, err = opts.Handle(workCtx, job)
	}()
	close(done)
	if renewalError := <-renewed; renewalError != nil {
		report(renewalError)
		return
	}
	if ctx.Err() != nil {
		return
	}
	action := "complete"
	finish := &WorkLeaseRequest{WorkerID: opts.WorkerID, LockToken: job.LockToken, Variables: variables}
	if err != nil {
		action = "fail"
		finish.Variables = nil
		finish.ErrorMessage = err.Error()
		var business *BPMNError
		if errors.As(err, &business) {
			action = "bpmn-error"
			finish.ErrorCode = business.Code
			finish.ErrorMessage = business.Message
		}
	}
	report(c.SettleWork(ctx, job.TaskID, action, finish))
}
