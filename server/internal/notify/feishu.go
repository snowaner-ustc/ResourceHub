package notify

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Feishu struct {
	webhook string
	secret  string
	client  *http.Client
	async   bool
}

func NewFeishu(webhook, secret string, async bool) *Feishu {
	webhook = strings.TrimSpace(webhook)
	secret = strings.TrimSpace(secret)
	if webhook == "" || !validFeishuWebhook(webhook) {
		return &Feishu{}
	}
	return &Feishu{
		webhook: webhook,
		secret:  secret,
		client:  &http.Client{Timeout: 5 * time.Second},
		async:   async,
	}
}

func (f *Feishu) Enabled() bool {
	return f != nil && f.webhook != ""
}

func (f *Feishu) Notify(ev Event) {
	if !f.Enabled() {
		return
	}
	send := func() {
		if err := f.post(ev); err != nil {
			log.Printf("feishu notify: %v", err)
		}
	}
	if f.async {
		go send()
		return
	}
	send()
}

func (f *Feishu) post(ev Event) error {
	body := map[string]any{
		"msg_type": "text",
		"content":  map[string]string{"text": FormatText(ev)},
	}
	if f.secret != "" {
		ts := time.Now().Unix()
		sign, err := feishuSign(f.secret, ts)
		if err != nil {
			return err
		}
		body["timestamp"] = fmt.Sprintf("%d", ts)
		body["sign"] = sign
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	resp, err := f.client.Post(f.webhook, "application/json", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook status %s", resp.Status)
	}
	var out struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err == nil && out.Code != 0 {
		return fmt.Errorf("feishu code %d %s", out.Code, out.Msg)
	}
	return nil
}

func feishuSign(secret string, timestamp int64) (string, error) {
	stringToSign := fmt.Sprintf("%d\n%s", timestamp, secret)
	h := hmac.New(sha256.New, []byte(stringToSign))
	sum := h.Sum(nil)
	return base64.StdEncoding.EncodeToString(sum), nil
}

func validFeishuWebhook(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" {
		return false
	}
	host := strings.ToLower(u.Host)
	if host != "open.feishu.cn" && host != "open.larksuite.com" {
		return false
	}
	return strings.HasPrefix(u.Path, "/open-apis/bot/v2/hook/") && len(strings.Trim(u.Path, "/")) > len("open-apis/bot/v2/hook")
}

func FromEnv(notifyFlag, webhook, secret string) Notifier {
	on := strings.TrimSpace(webhook) != ""
	if t := ParseToggle(notifyFlag); t != nil {
		on = *t
	}
	if !on {
		return Nop{}
	}
	f := NewFeishu(webhook, secret, true)
	if !f.Enabled() {
		if strings.TrimSpace(webhook) != "" {
			log.Printf("feishu notify requested but webhook is missing or not a Feishu bot hook URL")
		} else {
			log.Printf("feishu notify on, but RESOURCEHUB_FEISHU_WEBHOOK is empty")
		}
		return Nop{}
	}
	return f
}
