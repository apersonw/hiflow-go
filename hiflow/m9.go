package hiflow

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type PreviewRequest struct {
	Key         string         `json:"key"`
	Version     int            `json:"version,omitempty"`
	Starter     string         `json:"starter,omitempty"`
	BusinessKey string         `json:"business_key,omitempty"`
	Variables   map[string]any `json:"variables,omitempty"`
}
type Preview struct {
	Startable    *bool            `json:"startable"`
	Reason       string           `json:"reason"`
	Steps        []PreviewStep    `json:"steps"`
	Undetermined []PreviewPending `json:"undetermined"`
	Ends         []PreviewEnd     `json:"ends"`
}
type PreviewStep struct {
	Sequential bool     `json:"sequential"`
	NodeID     string   `json:"node_id"`
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	Status     string   `json:"status"`
	Users      []string `json:"users"`
	Mode       string   `json:"mode"`
	Pending    bool     `json:"pending"`
	Note       string   `json:"note"`
}
type PreviewPending struct {
	NodeID   string       `json:"node_id"`
	Name     string       `json:"name"`
	Reason   string       `json:"reason"`
	Branches []PreviewEnd `json:"branches"`
}
type PreviewEnd struct {
	NodeID string `json:"node_id"`
	Name   string `json:"name"`
	Result string `json:"result"`
}
type StartableDefinition struct {
	ID         int64  `json:"id"`
	Key        string `json:"key"`
	Name       string `json:"name"`
	Version    int    `json:"version"`
	Restricted bool   `json:"restricted"`
}
type TaskDraft struct {
	TaskID    int64          `json:"task_id"`
	Operator  string         `json:"operator"`
	Variables map[string]any `json:"variables"`
	UpdatedAt time.Time      `json:"updated_at"`
}
type TaskComment struct {
	ID          int64            `json:"id"`
	TaskID      int64            `json:"task_id"`
	Operator    string           `json:"operator"`
	ParentID    *int64           `json:"parent_id"`
	Content     string           `json:"content"`
	Mentions    []string         `json:"mentions"`
	Attachments []map[string]any `json:"attachments"`
	CreatedAt   time.Time        `json:"created_at"`
}
type CommentRequest struct {
	Operator    string           `json:"operator"`
	ParentID    *int64           `json:"parent_id,omitempty"`
	Content     string           `json:"content"`
	Mentions    []string         `json:"mentions,omitempty"`
	Attachments []map[string]any `json:"attachments,omitempty"`
}

func (c *Client) PreviewStart(ctx context.Context, req *PreviewRequest) (*Preview, error) {
	var out Preview
	err := c.post(ctx, "/open/v1/instances/preview", req, &out)
	return &out, err
}
func (c *Client) PreviewInstance(ctx context.Context, id int64) (*Preview, error) {
	var out Preview
	err := c.get(ctx, "/open/v1/instances/"+strconv.FormatInt(id, 10)+"/preview", nil, &out)
	return &out, err
}
func (c *Client) StartableDefinitions(ctx context.Context, user string) ([]StartableDefinition, error) {
	var out []StartableDefinition
	err := c.get(ctx, "/open/v1/definitions", url.Values{"startable_by": {user}}, &out)
	return out, err
}
func (c *Client) TaskDraft(ctx context.Context, id int64, operator string) (*TaskDraft, error) {
	var out TaskDraft
	err := c.get(ctx, "/open/v1/tasks/"+strconv.FormatInt(id, 10)+"/draft", url.Values{"operator": {operator}}, &out)
	return &out, err
}
func (c *Client) SaveTaskDraft(ctx context.Context, id int64, operator string, vars map[string]any) (*TaskDraft, error) {
	var out TaskDraft
	b, err := json.Marshal(map[string]any{"operator": operator, "variables": vars})
	if err != nil {
		return nil, err
	}
	err = c.call(ctx, &request{method: http.MethodPut, path: "/open/v1/tasks/" + strconv.FormatInt(id, 10) + "/draft", body: b}, &out)
	return &out, err
}
func (c *Client) CommentTask(ctx context.Context, id int64, req *CommentRequest) (*TaskComment, error) {
	var out TaskComment
	err := c.post(ctx, "/open/v1/tasks/"+strconv.FormatInt(id, 10)+"/comments", req, &out)
	return &out, err
}
func (c *Client) TaskComments(ctx context.Context, id, after int64) ([]TaskComment, error) {
	var out []TaskComment
	err := c.get(ctx, "/open/v1/tasks/"+strconv.FormatInt(id, 10)+"/comments", url.Values{"after": {strconv.FormatInt(after, 10)}}, &out)
	return out, err
}
