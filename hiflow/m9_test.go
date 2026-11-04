package hiflow

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestM9SignedDraftAndPreview(t *testing.T) {
	srv := fakeServer(t, func(r *http.Request, body []byte) (any, *Error) {
		switch r.URL.Path {
		case "/api/open/v1/tasks/7/draft":
			if r.Method != http.MethodPut {
				t.Errorf("method %s", r.Method)
			}
			var req map[string]any
			if err := json.Unmarshal(body, &req); err != nil || req["operator"] != "审批人甲" {
				t.Errorf("body %s", body)
			}
			return map[string]any{"task_id": 7, "operator": "审批人甲", "variables": map[string]any{"answer": false}}, nil
		case "/api/open/v1/instances/preview":
			return map[string]any{"startable": false, "reason": "无权限", "steps": []any{map[string]any{"node_id": "a", "users": []string{"boss"}, "pending": true}}, "undetermined": []any{}, "ends": []any{}}, nil
		}
		return nil, &Error{StatusCode: 404, Code: CodeNotFound}
	})
	c := newTestClient(t, srv)
	ctx := context.Background()
	d, err := c.SaveTaskDraft(ctx, 7, "审批人甲", map[string]any{"answer": false})
	if err != nil || d.TaskID != 7 || d.Variables["answer"] != false {
		t.Fatalf("draft %+v %v", d, err)
	}
	p, err := c.PreviewStart(ctx, &PreviewRequest{Key: "test"})
	if err != nil || p.Startable == nil || *p.Startable || len(p.Steps) != 1 || !p.Steps[0].Pending {
		t.Fatalf("preview %+v %v", p, err)
	}
}
