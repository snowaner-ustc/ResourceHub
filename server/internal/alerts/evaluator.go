package alerts

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/snowaner-ustc/ResourceHub/server/internal/models"
	"github.com/snowaner-ustc/ResourceHub/server/internal/store"
)

type Config struct {
	DiskWarningPercent  float64
	DiskCriticalPercent float64
	CPUWarningPercent   float64
	CPUCriticalPercent  float64
	MemWarningPercent   float64
	MemCriticalPercent  float64
	GPUWarningPercent   float64
	GPUCriticalPercent  float64
	GPUMemWarnPercent   float64
	GPUMemCritPercent   float64
	ProcessCountWarning int
	ZombieCriticalCount int
	ZombiePersistAfter  time.Duration
	GaugePersistFor     time.Duration
}

func DefaultConfig() Config {
	return Config{
		DiskWarningPercent:  80,
		DiskCriticalPercent: 90,
		CPUWarningPercent:   80,
		CPUCriticalPercent:  95,
		MemWarningPercent:   85,
		MemCriticalPercent:  95,
		GPUWarningPercent:   90,
		GPUCriticalPercent:  98,
		GPUMemWarnPercent:   90,
		GPUMemCritPercent:   95,
		ProcessCountWarning: 2000,
		ZombieCriticalCount: 10,
		ZombiePersistAfter:  5 * time.Minute,
		GaugePersistFor:     3 * time.Minute,
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
	if err := e.evaluateGauges(host, snap); err != nil {
		return err
	}
	return nil
}

func (e *Evaluator) evaluateGauges(host *models.Host, snap *models.MetricSnapshot) error {
	if err := e.gauge(host, "cpu.high", "cpu.usage_percent", snap.CPU.UsagePercent,
		e.config.CPUWarningPercent, e.config.CPUCriticalPercent, "CPU"); err != nil {
		return err
	}
	if err := e.gauge(host, "memory.high", "memory.used_percent", snap.Memory.UsedPercent,
		e.config.MemWarningPercent, e.config.MemCriticalPercent, "memory"); err != nil {
		return err
	}
	if snap.GPUs == nil || !snap.GPUs.Available {
		_ = e.store.ResolveAlerts(host.ID, "gpu.high")
		_ = e.store.ResolveAlerts(host.ID, "gpu.memory.high")
		return nil
	}
	var maxUtil, maxMem float64
	for _, g := range snap.GPUs.Devices {
		if g.UtilPercent > maxUtil {
			maxUtil = g.UtilPercent
		}
		if g.MemoryUsedPercent > maxMem {
			maxMem = g.MemoryUsedPercent
		}
	}
	if err := e.gauge(host, "gpu.high", "gpu.max_util_percent", maxUtil,
		e.config.GPUWarningPercent, e.config.GPUCriticalPercent, "GPU util"); err != nil {
		return err
	}
	if err := e.gauge(host, "gpu.memory.high", "gpu.max_mem_percent", maxMem,
		e.config.GPUMemWarnPercent, e.config.GPUMemCritPercent, "GPU memory"); err != nil {
		return err
	}
	return nil
}

func (e *Evaluator) gauge(host *models.Host, rule, metric string, current, warn, crit float64, label string) error {
	payload := fmt.Sprintf(`{"metric":%q,"value":%.2f}`, metric, current)
	if current >= crit {
		ok, err := e.store.SustainedAbove(host.ID, metric, crit, e.config.GaugePersistFor)
		if err != nil {
			return err
		}
		if ok {
			return e.fire(host, rule, models.AlertSeverityCritical,
				fmt.Sprintf("%s critical: %.1f%% on %s", label, current, host.Name), payload)
		}
	}
	if current >= warn {
		ok, err := e.store.SustainedAbove(host.ID, metric, warn, e.config.GaugePersistFor)
		if err != nil {
			return err
		}
		if ok {
			return e.fire(host, rule, models.AlertSeverityWarning,
				fmt.Sprintf("%s warning: %.1f%% on %s", label, current, host.Name), payload)
		}
		return nil
	}
	return e.store.ResolveAlerts(host.ID, rule)
}

func (e *Evaluator) evaluateProcesses(host *models.Host, snap *models.MetricSnapshot) error {
	if snap.Processes == nil {
		return nil
	}
	sum := snap.Processes.Summary
	payload, _ := json.Marshal(map[string]any{
		"zombie":  sum.Zombie,
		"total":   sum.Total,
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

// EvaluateQuota fires or resolves soft-quota alerts for the given usages.
// When complete is false (a partial scan), alerts for paths not in this batch
// are left unchanged so missing coverage cannot look like a recovery.
func (e *Evaluator) EvaluateQuota(host *models.Host, policy *models.QuotaPolicy, usages []models.QuotaUsage, complete bool) error {
	if policy == nil {
		return nil
	}
	prefix := models.QuotaRulePrefix(policy.ID)
	seen := map[string]struct{}{}
	for _, u := range usages {
		rule := models.QuotaRuleType(policy.ID, u.Path)
		seen[rule] = struct{}{}
		payload, _ := json.Marshal(map[string]any{
			"path":         u.Path,
			"owner":        u.Owner,
			"used_bytes":   u.UsedBytes,
			"limit_bytes":  u.LimitBytes,
			"used_percent": u.UsedPercent,
			"policy_id":    policy.ID,
			"soft_limit":   true,
		})
		msgSuffix := fmt.Sprintf("%s at %.1f%% of limit on %s (soft limit, writes still allowed)", u.Path, u.UsedPercent, host.Name)
		if u.UsedPercent >= policy.CriticalPercent {
			if err := e.fire(host, rule, models.AlertSeverityCritical, "soft quota critical: "+msgSuffix, string(payload)); err != nil {
				return err
			}
			continue
		}
		if u.UsedPercent >= policy.WarnPercent {
			if err := e.fire(host, rule, models.AlertSeverityWarning, "soft quota warning: "+msgSuffix, string(payload)); err != nil {
				return err
			}
			continue
		}
		if err := e.store.ResolveAlerts(host.ID, rule); err != nil {
			return err
		}
	}
	if !complete {
		return nil
	}
	firing, err := e.store.ListFiringAlertsByHost(host.ID)
	if err != nil {
		return err
	}
	for _, a := range firing {
		if strings.HasPrefix(a.RuleType, prefix) {
			if _, ok := seen[a.RuleType]; !ok {
				if err := e.store.ResolveAlerts(host.ID, a.RuleType); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
