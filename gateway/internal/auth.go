package internal

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
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

// Verify 校验 X-Signature-256 header。验证后恢复 body 供下游读取。
func (a *WebhookAuth) Verify(r *http.Request) error {
	if len(a.secret) == 0 {
		return nil
	}
	sig := r.Header.Get("X-Signature-256")
	if sig == "" {
		return errors.New("missing signature header")
	}
	if !strings.HasPrefix(sig, "sha256=") {
		return errors.New("invalid signature format")
	}

	body, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		return err
	}
	// 恢复 body 供 relay.Forward 等下游使用
	r.Body = io.NopCloser(bytes.NewReader(body))

	mac := hmac.New(sha256.New, a.secret)
	mac.Write(body)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return errors.New("signature mismatch")
	}
	return nil
}
