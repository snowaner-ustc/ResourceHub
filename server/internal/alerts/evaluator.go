package alerts

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/snowaner-ustc/ResourceHub/server/internal/models"
	"github.com/snowaner-ustc/ResourceHub/server/internal/store"
)

type Config struct {
	DiskWarningPercent   float64
	DiskCriticalPercent  float64
	ProcessCountWarning  int
	ZombieCriticalCount  int
	ZombiePersistAfter   time.Duration
}

func DefaultConfig() Config {
	return Config{
		DiskWarningPercent:  80,
		DiskCriticalPercent: 90,
		ProcessCountWarning: 2000,
		ZombieCriticalCount: 10,
		ZombiePersistAfter:  5 * time.Minute,
	}
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
	if err := e.evaluateProcesses(host, snap); err != nil {
		return err
	}
	return nil
}

func (e *Evaluator) evaluateProcesses(host *models.Host, snap *models.MetricSnapshot) error {
	if snap.Processes == nil {
		return nil
	}
	sum := snap.Processes.Summary
	payload, _ := json.Marshal(map[string]any{
		"zombie": sum.Zombie,
		"total":  sum.Total,
		"zombies": snap.Processes.Zombies,
	})

	if sum.Zombie >= 1 {
		if err := e.fire(host, "process.zombie", models.AlertSeverityWarning,
			fmt.Sprintf("zombie processes: %d on %s", sum.Zombie, host.Name), string(payload)); err != nil {
			return err
		}
		existing, err := e.store.GetFiringAlert(host.ID, "process.zombie")
		if err != nil {
			return err
		}
		if existing != nil && time.Since(existing.FiredAt) >= e.config.ZombiePersistAfter {
			if err := e.fire(host, "process.zombie.persistent", models.AlertSeverityCritical,
				fmt.Sprintf("zombie processes persist >= %s: %d on %s", e.config.ZombiePersistAfter, sum.Zombie, host.Name),
				string(payload)); err != nil {
				return err
			}
		}
	} else {
		if err := e.store.ResolveAlerts(host.ID, "process.zombie"); err != nil {
			return err
		}
		if err := e.store.ResolveAlerts(host.ID, "process.zombie.persistent"); err != nil {
			return err
		}
		if err := e.store.ResolveAlerts(host.ID, "process.zombie.high"); err != nil {
			return err
		}
	}

	if sum.Zombie >= e.config.ZombieCriticalCount {
		if err := e.fire(host, "process.zombie.high", models.AlertSeverityCritical,
			fmt.Sprintf("high zombie count: %d on %s", sum.Zombie, host.Name), string(payload)); err != nil {
			return err
		}
	} else if sum.Zombie > 0 {
		if err := e.store.ResolveAlerts(host.ID, "process.zombie.high"); err != nil {
			return err
		}
	}

	ruleCount := "process.count.high"
	if e.config.ProcessCountWarning > 0 && sum.Total > e.config.ProcessCountWarning {
		if err := e.fire(host, ruleCount, models.AlertSeverityWarning,
			fmt.Sprintf("process count high: %d > %d on %s", sum.Total, e.config.ProcessCountWarning, host.Name),
			string(payload)); err != nil {
			return err
		}
	} else {
		if err := e.store.ResolveAlerts(host.ID, ruleCount); err != nil {
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
	if existing != nil && existing.Severity == severity && existing.Message == message {
		return nil
	}
	now := time.Now().UTC()
	id := e.store.NewAlertID()
	firedAt := now
	if existing != nil {
		id = existing.ID
		firedAt = existing.FiredAt
	}
	return e.store.UpsertAlert(models.Alert{
		ID:       id,
		HostID:   host.ID,
		HostName: host.Name,
		RuleType: ruleType,
		Severity: severity,
		Status:   models.AlertStatusFiring,
		Message:  message,
		Payload:  payload,
		FiredAt:  firedAt,
	})
}

func (e *Evaluator) EvaluateOffline(host *models.Host) error {
	return e.fire(host, "agent:offline", models.AlertSeverityCritical,
		fmt.Sprintf("agent offline: %s", host.Name), `{}`)
}

func (e *Evaluator) ResolveOffline(hostID string) error {
	return e.store.ResolveAlerts(hostID, "agent:offline")
}
