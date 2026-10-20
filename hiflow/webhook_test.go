package hiflow

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func signFor(secret string, ts int64, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.", ts)
	mac.Write([]byte(body))
	return fmt.Sprintf("t=%d,v1=%s", ts, hex.EncodeToString(mac.Sum(nil)))
}

const sampleEvent = `{"id":"e1","type":"task.created","created_at":"2026-10-20T07:00:00Z",
"data":{"instance":{"id":7,"definition_key":"leave","business_key":"L-1","status":"running"},
"task":{"id":11,"node_id":"manager","name":"经理审批","assignee":"u7","candidates":[],"status":"active"}}}`

// 与服务端 internal/service/webhook_sign_test.go、Java SDK WebhookVerifierTest 共用的签名向量
const (
	vectorSecret    = "whsec_test"
	vectorBody      = `{"id":"e1","type":"webhook.test","created_at":"2026-10-20T07:00:00Z","data":{}}`
	vectorSignature = "t=1700000000,v1=%s"
)

func TestWebhookSignatureVector(t *testing.T) {
	sig := signFor(vectorSecret, 1700000000, vectorBody)
	// 签名时间很早：放宽允许偏差，只校验签名算法
	e, err := VerifyWebhook(vectorSecret, sig, []byte(vectorBody), 100*365*24*time.Hour)
	if err != nil || e.Type != EventWebhookTest {
		t.Fatalf("vector: %v", err)
	}
	if sig != fmt.Sprintf(vectorSignature, "125598be773b84023832595d02264cbe20d926bab5c9f0e5c74bc164b1d8e226") {
		t.Fatalf("vector signature = %s", sig)
	}
}

func TestVerifyWebhook(t *testing.T) {
	now := time.Now().Unix()
	e, err := VerifyWebhook("whsec_x", signFor("whsec_x", now, sampleEvent), []byte(sampleEvent), 0)
	if err != nil {
		t.Fatal(err)
	}
	d, err := e.Flow()
	if err != nil || d.Instance.BusinessKey != "L-1" || d.Task == nil || d.Task.Assignee != "u7" {
		t.Fatalf("flow = %+v, %v", d, err)
	}

	if _, err := VerifyWebhook("other", signFor("whsec_x", now, sampleEvent), []byte(sampleEvent), 0); !errors.Is(err, ErrWebhookSignature) {
		t.Fatalf("wrong secret: %v", err)
	}
	if _, err := VerifyWebhook("whsec_x", signFor("whsec_x", now, sampleEvent), []byte(sampleEvent+" "), 0); !errors.Is(err, ErrWebhookSignature) {
		t.Fatalf("tampered body: %v", err)
	}
	old := now - 3600
	if _, err := VerifyWebhook("whsec_x", signFor("whsec_x", old, sampleEvent), []byte(sampleEvent), 0); !errors.Is(err, ErrWebhookTimestamp) {
		t.Fatalf("stale: %v", err)
	}
	if _, err := VerifyWebhook("whsec_x", "garbage", []byte(sampleEvent), 0); !errors.Is(err, ErrWebhookSignature) {
		t.Fatalf("garbage: %v", err)
	}
}

func TestWebhookHandler(t *testing.T) {
	var got *WebhookEvent
	h := WebhookHandler("whsec_x", func(_ *http.Request, e *WebhookEvent) error {
		got = e
		return nil
	})
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(sampleEvent))
	req.Header.Set(SignatureHeader, signFor("whsec_x", time.Now().Unix(), sampleEvent))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent || got == nil || got.Type != EventTaskCreated {
		t.Fatalf("code = %d, event = %+v", rec.Code, got)
	}
	bad := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(sampleEvent))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, bad)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unsigned code = %d", rec.Code)
	}
}
