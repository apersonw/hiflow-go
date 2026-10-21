package hiflow

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// 与服务端 pkg/embedtoken、Java SDK、浏览器 SDK 共用的签名向量
func TestEmbedTokenVector(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"client_id":"hf_test","user":"u1","nonce":"0123456789abcdef","expires_at":1700000000}`))
	if got := embedTokenFromPayload("sk_test_embed", payload); got != "eyJjbGllbnRfaWQiOiJoZl90ZXN0IiwidXNlciI6InUxIiwibm9uY2UiOiIwMTIzNDU2Nzg5YWJjZGVmIiwiZXhwaXJlc19hdCI6MTcwMDAwMDAwMH0.fYN-4Mukn5H-7ZT23S4QVOeVjXal60YCqRDivVPefbg" {
		t.Fatalf("token = %s", got)
	}
}

func TestNewEmbedToken(t *testing.T) {
	c, _ := New("https://flow.example.com", "hf_x", "sk")
	c.clock = func() time.Time { return time.Unix(1700000000, 0) }
	tok, err := c.NewEmbedToken(&EmbedTokenOptions{User: "u1", Actions: []string{EmbedComplete}, ExpiresIn: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	payload, _, _ := strings.Cut(tok.Token, ".")
	raw, _ := base64.RawURLEncoding.DecodeString(payload)
	var claims embedClaims
	_ = json.Unmarshal(raw, &claims)
	if claims.ClientID != "hf_x" || claims.User != "u1" || len(claims.Nonce) != 32 || claims.ExpiresAt != 1700003600 || tok.ExpiresAt.Unix() != 1700003600 {
		t.Fatalf("claims = %+v", claims)
	}
	if embedTokenFromPayload("sk", payload) != tok.Token {
		t.Fatal("signature")
	}
	if _, err := c.NewEmbedToken(&EmbedTokenOptions{}); err == nil {
		t.Fatal("user required")
	}
	if _, err := c.NewEmbedToken(&EmbedTokenOptions{User: "u", ExpiresIn: 48 * time.Hour}); err == nil {
		t.Fatal("ttl cap")
	}
}
