package internal

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
)

// WebhookAuth 验证 Webhook 请求的 HMAC 签名。
type WebhookAuth struct {
	secret []byte
}

func NewWebhookAuth(secret string) *WebhookAuth {
	return &WebhookAuth{secret: []byte(secret)}
}

// Verify 校验 X-Signature-256 header。
func (a *WebhookAuth) Verify(r *http.Request) error {
	if len(a.secret) == 0 {
		return nil // 未配置密钥则跳过验证
	}
	sig := r.Header.Get("X-Signature-256")
	if sig == "" {
		return errors.New("missing signature header")
	}
	// 预期: sha256=<hex>
	if !strings.HasPrefix(sig, "sha256=") {
		return errors.New("invalid signature format")
	}
	body := []byte{}
	if r.Body != nil {
		// 注意：需要缓存 body 以供后续读取
		buf := make([]byte, 0)
		tmp := make([]byte, 1024)
		for {
			n, err := r.Body.Read(tmp)
			buf = append(buf, tmp[:n]...)
			if err != nil {
				break
			}
		}
		body = buf
	}
	mac := hmac.New(sha256.New, a.secret)
	mac.Write(body)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return errors.New("signature mismatch")
	}
	return nil
}
