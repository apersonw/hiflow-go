package hiflow

import (
	"net/url"
	"testing"
)

// 与服务端 pkg/signer/signer_test.go、Java SDK SignerTest.java 共用的签名向量
func TestSharedVector(t *testing.T) {
	body := []byte(`{"operator":"alice","variables":{"approved":true}}`)
	hash := hashHex(body)
	if hash != "e1bdad04d8c6bc180c60bef1d8454510a154a9d755a28de93d50dcc6b5c6536c" {
		t.Fatalf("hash = %s", hash)
	}
	sts := stringToSign("post", "/open/v1/tasks/42/complete", url.Values{"b": {"2", "1"}, "a b": {"x y~"}},
		"hf_test", "1700000000", "0123456789abcdef0123456789abcdef", hash)
	want := "POST\n/open/v1/tasks/42/complete\na%20b=x%20y~&b=1&b=2\nhf_test\n1700000000\n0123456789abcdef0123456789abcdef\n" + hash
	if sts != want {
		t.Fatalf("string to sign = %q", sts)
	}
	if got := sign("test-secret", sts); got != "NQQ/+k+GIQ+iNJdFDfLY1hRJjE87RyPSTYGF7GgtSlo=" {
		t.Fatalf("signature = %s", got)
	}
}
