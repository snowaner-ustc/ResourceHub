package alerts

import (
	"fmt"
	"time"

	"github.com/snowaner-ustc/ResourceHub/server/internal/models"
	"github.com/snowaner-ustc/ResourceHub/server/internal/store"
)

type Config struct {
	DiskWarningPercent  float64
	DiskCriticalPercent float64
}

func DefaultConfig() Config {
	return Config{DiskWarningPercent: 80, DiskCriticalPercent: 90}
}

type Evaluator struct {
	store  *store.Store
	config Config
}

func New(st *store.Store, cfg Config) *Evaluator {
	return &Evaluator{store: st, config: cfg}
}

func (e *Evaluator) EvaluateSnapshot(host *models.Host, snap *models.MetricSnapshot) error {
	if snap == nil {
		return nil
	}
	for _, d := range snap.Disks {
		ruleType := fmt.Sprintf("disk:%s", d.Mountpoint)
		if d.UsedPercent >= e.config.DiskCriticalPercent {
			if err := e.fire(host, ruleType, models.AlertSeverityCritical,
				fmt.Sprintf("disk critical: %s at %.1f%%", d.Mountpoint, d.UsedPercent),
				fmt.Sprintf(`{"mountpoint":%q,"used_percent":%.2f}`, d.Mountpoint, d.UsedPercent)); err != nil {
				return err
			}
			continue
		}
		if d.UsedPercent >= e.config.DiskWarningPercent {
			if err := e.fire(host, ruleType, models.AlertSeverityWarning,
				fmt.Sprintf("disk warning: %s at %.1f%%", d.Mountpoint, d.UsedPercent),
				fmt.Sprintf(`{"mountpoint":%q,"used_percent":%.2f}`, d.Mountpoint, d.UsedPercent)); err != nil {
				return err
			}
			continue
		}
		if err := e.store.ResolveAlerts(host.ID, ruleType); err != nil {
			return err
		}
	}
	return nil
}

func (e *Evaluator) fire(host *models.Host, ruleType string, severity models.AlertSeverity, message, payload string) error {
	existing, err := e.store.GetFiringAlert(host.ID, ruleType)
	if err != nil {
		return err
	}
	if existing != nil && existing.Severity == severity {
		return nil
	}
	now := time.Now().UTC()
	return e.store.UpsertAlert(models.Alert{
		ID:       e.store.NewAlertID(),
		HostID:   host.ID,
		HostName: host.Name,
		RuleType: ruleType,
		Severity: severity,
		Status:   models.AlertStatusFiring,
		Message:  message,
		Payload:  payload,
		FiredAt:  now,
	})
}

func (e *Evaluator) EvaluateOffline(host *models.Host) error {
	return e.fire(host, "agent:offline", models.AlertSeverityCritical,
		fmt.Sprintf("agent offline: %s", host.Name), `{}`)
}

func (e *Evaluator) ResolveOffline(hostID string) error {
	return e.store.ResolveAlerts(hostID, "agent:offline")
}
