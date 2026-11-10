package notify

import (
	"strings"

	"github.com/snowaner-ustc/ResourceHub/server/internal/models"
)

const (
	KindFiring   = "firing"
	KindResolved = "resolved"
)

type Event struct {
	Kind     string
	HostID   string
	HostName string
	RuleType string
	Severity models.AlertSeverity
	Message  string
}

type Notifier interface {
	Notify(Event)
	Enabled() bool
}

type Nop struct{}

func (Nop) Notify(Event)  {}
func (Nop) Enabled() bool { return false }

func FormatText(ev Event) string {
	host := ev.HostName
	if host == "" {
		host = ev.HostID
	}
	if ev.Kind == KindResolved {
		return "[ResourceHub] resolved " + host + " " + ev.RuleType
	}
	sev := string(ev.Severity)
	if sev == "" {
		sev = "warning"
	}
	msg := ev.Message
	if msg == "" {
		msg = ev.RuleType
	}
	return "[ResourceHub] firing " + sev + " " + host + "\n" + msg
}

func ParseToggle(v string) *bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "on", "yes":
		t := true
		return &t
	case "0", "false", "off", "no":
		t := false
		return &t
	default:
		return nil
	}
}
