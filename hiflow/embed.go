package hiflow

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

// 嵌入令牌可授予的操作
const (
	EmbedComplete = "complete"
	EmbedClaim    = "claim"
	EmbedTransfer = "transfer"
	EmbedReturn   = "return"
)

const embedSigningPrefix = "hiflow-embed-token/v1\n"

// MaxEmbedTTL 嵌入令牌最长有效期
const MaxEmbedTTL = 24 * time.Hour

// EmbedTokenOptions 待办嵌入令牌：业务前端（浏览器 SDK / 嵌入组件）凭它以 User 的身份查看与处理他的待办
type EmbedTokenOptions struct {
	// User 令牌代表的业务用户（通常是当前登录用户），必填
	User string
	// ExpiresIn 有效期，默认 2 小时，最长 24 小时
	ExpiresIn time.Duration
	// Actions 允许的操作（EmbedComplete 等），为空表示全部
	Actions []string
	// Variables 处理任务时允许提交的流程变量名，为空时只允许 approved；[]string{"*"} 表示不限
	Variables []string
	// ReadVariables 任务详情中是否返回流程变量
	ReadVariables bool
}

// EmbedToken 签发的嵌入令牌
type EmbedToken struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

type embedClaims struct {
	ClientID      string   `json:"client_id"`
	User          string   `json:"user"`
	Nonce         string   `json:"nonce"`
	ExpiresAt     int64    `json:"expires_at"`
	Actions       []string `json:"actions,omitempty"`
	Variables     []string `json:"variables,omitempty"`
	ReadVariables bool     `json:"read_variables,omitempty"`
}

// NewEmbedToken 用客户端密钥在本地签发待办嵌入令牌（不请求 HiFlow），把 Token 返回给业务前端。
// 令牌格式：base64url(JSON) + "." + base64url(HMAC-SHA256(secret, "hiflow-embed-token/v1\n" + payload))
func (c *Client) NewEmbedToken(opts *EmbedTokenOptions) (*EmbedToken, error) {
	if opts == nil || opts.User == "" || len(opts.User) > 64 {
		return nil, errors.New("hiflow: embed token user is required (at most 64 characters)")
	}
	ttl := opts.ExpiresIn
	if ttl <= 0 {
		ttl = 2 * time.Hour
	}
	if ttl > MaxEmbedTTL {
		return nil, errors.New("hiflow: embed token ttl must not exceed 24h")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	exp := c.now().Add(ttl).Truncate(time.Second)
	token, err := signEmbed(c.secret, embedClaims{
		ClientID: c.clientID, User: opts.User, Nonce: hex.EncodeToString(nonce[:]), ExpiresAt: exp.Unix(),
		Actions: opts.Actions, Variables: opts.Variables, ReadVariables: opts.ReadVariables,
	})
	if err != nil {
		return nil, err
	}
	return &EmbedToken{Token: token, ExpiresAt: exp}, nil
}

func signEmbed(secret string, claims embedClaims) (string, error) {
	raw, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	return embedTokenFromPayload(secret, base64.RawURLEncoding.EncodeToString(raw)), nil
}

func embedTokenFromPayload(secret, payload string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(embedSigningPrefix))
	m.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
