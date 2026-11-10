package notify

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/snowaner-ustc/ResourceHub/server/internal/models"
)

func TestFromEnvOffByDefault(t *testing.T) {
	n := FromEnv("", "", "")
	if n.Enabled() {
		t.Fatal("expected disabled")
	}
	n = FromEnv("0", "https://open.feishu.cn/open-apis/bot/v2/hook/abc", "")
	if n.Enabled() {
		t.Fatal("toggle off should win")
	}
	n = FromEnv("true", "", "")
	if n.Enabled() {
		t.Fatal("on without webhook should be nop")
	}
	n = FromEnv("", "http://127.0.0.1/hook", "")
	if n.Enabled() {
		t.Fatal("non-feishu URL must be rejected")
	}
	n = FromEnv("", "https://open.feishu.cn/open-apis/bot/v2/hook/abc", "")
	if !n.Enabled() {
		t.Fatal("webhook without toggle should enable")
	}
	n = FromEnv("1", "https://open.larksuite.com/open-apis/bot/v2/hook/abc", "s")
	if !n.Enabled() {
		t.Fatal("explicit on + valid webhook")
	}
}

func TestFormatTextKeywordPrefix(t *testing.T) {
	got := FormatText(Event{Kind: KindFiring, HostName: "gpu1", Severity: models.AlertSeverityCritical, Message: "disk full"})
	if got != "[ResourceHub] firing critical gpu1\ndisk full" {
		t.Fatalf("firing: %q", got)
	}
	got = FormatText(Event{Kind: KindResolved, HostName: "gpu1", RuleType: "disk:/data"})
	if got != "[ResourceHub] resolved gpu1 disk:/data" {
		t.Fatalf("resolved: %q", got)
	}
}

func TestFeishuPostsTextAndSign(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok"}`))
	}))
	defer srv.Close()

	f := &Feishu{webhook: srv.URL, secret: "s3cret", client: srv.Client(), async: false}
	f.Notify(Event{
		Kind: KindFiring, HostName: "gpu1", RuleType: "disk:/data",
		Severity: models.AlertSeverityCritical, Message: "disk critical: /data at 93.0%",
	})
	text, _ := got["content"].(map[string]any)["text"].(string)
	if got["msg_type"] != "text" || text == "" {
		t.Fatalf("body %+v", got)
	}
	if text[:13] != "[ResourceHub]" {
		t.Fatalf("keyword prefix: %q", text)
	}
	if got["sign"] == nil || got["timestamp"] == nil {
		t.Fatalf("expected sign, got %+v", got)
	}
}

func TestDisabledNotifyDoesNotHitNetwork(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
	}))
	defer srv.Close()
	var n Notifier = Nop{}
	n.Notify(Event{Kind: KindFiring, Message: "x"})
	if hit {
		t.Fatal("nop should not HTTP")
	}
	f := NewFeishu("", "", false)
	f.Notify(Event{Kind: KindFiring, Message: "x"})
	if hit {
		t.Fatal("empty webhook should not HTTP")
	}
}
