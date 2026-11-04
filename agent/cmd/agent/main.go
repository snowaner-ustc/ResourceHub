package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/snowaner-ustc/ResourceHub/agent/internal/collector"
	"github.com/snowaner-ustc/ResourceHub/agent/internal/reporter"
)

const agentVersion = "0.3.0"

func main() {
	serverURL := flag.String("server", "http://127.0.0.1:8080", "ResourceHub server URL")
	name := flag.String("name", "", "display name (defaults to hostname)")
	group := flag.String("group", "", "optional host group label")
	interval := flag.Duration("interval", 10*time.Second, "metrics report interval")
	procInterval := flag.Duration("proc-interval", 30*time.Second, "process scan interval (mid-path)")
	gpuInterval := flag.Duration("gpu-interval", 15*time.Second, "GPU scan interval")
	tokenFile := flag.String("token-file", ".resourcehub-token", "persisted agent token path")
	flag.Parse()

	hostname, err := os.Hostname()
	if err != nil {
		log.Fatalf("hostname: %v", err)
	}
	if *name == "" {
		*name = hostname
	}

	client := reporter.New(*serverURL)
	if token, err := reporter.LoadToken(*tokenFile); err == nil && token != "" {
		client.SetToken(token)
		log.Printf("loaded token from %s", *tokenFile)
	} else {
		resp, err := client.Register(*name, hostname, agentVersion, *group)
		if err != nil {
			log.Fatalf("register: %v", err)
		}
		if err := reporter.SaveToken(*tokenFile, resp.Token); err != nil {
			log.Fatalf("save token: %v", err)
		}
		log.Printf("registered host_id=%s group=%q", resp.HostID, *group)
	}

	col := collector.New()
	if _, err := col.Collect(); err != nil {
		log.Printf("initial collect warning: %v", err)
	}
	proc := col.CollectProcesses()
	log.Printf("initial process scan: total=%d zombie=%d duration=%dms partial=%v",
		proc.Summary.Total, proc.Summary.Zombie, proc.Summary.CollectDurationMs, proc.Summary.Partial)
	gpu := col.CollectGPUs()
	log.Printf("initial gpu scan: available=%v devices=%d method=%s", gpu.Available, len(gpu.Devices), gpu.Method)

	ticker := time.NewTicker(*interval)
	defer ticker.Stop()
	procTicker := time.NewTicker(*procInterval)
	defer procTicker.Stop()
	gpuTicker := time.NewTicker(*gpuInterval)
	defer gpuTicker.Stop()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	report := func() {
		snap, err := col.Collect()
		if err != nil {
			log.Printf("collect error: %v", err)
			return
		}
		if err := client.Report(snap); err != nil {
			log.Printf("report error: %v", err)
			return
		}
		zombie := 0
		total := 0
		if snap.Processes != nil {
			zombie = snap.Processes.Summary.Zombie
			total = snap.Processes.Summary.Total
		}
		gpus := 0
		if snap.GPUs != nil {
			gpus = len(snap.GPUs.Devices)
		}
		log.Printf("reported cpu=%.1f%% mem=%.1f%% disks=%d gpus=%d procs=%d zombies=%d duration=%dms",
			snap.CPU.UsagePercent, snap.Memory.UsedPercent, len(snap.Disks), gpus, total, zombie, snap.CollectDurationMs)
	}

	report()
	for {
		select {
		case <-ticker.C:
			report()
		case <-procTicker.C:
			p := col.CollectProcesses()
			log.Printf("process scan: total=%d zombie=%d top_cpu=%d duration=%dms partial=%v",
				p.Summary.Total, p.Summary.Zombie, len(p.TopCPU), p.Summary.CollectDurationMs, p.Summary.Partial)
			report()
		case <-gpuTicker.C:
			g := col.CollectGPUs()
			log.Printf("gpu scan: available=%v devices=%d duration=%dms", g.Available, len(g.Devices), g.CollectDurationMs)
			report()
		case <-sig:
			return
		}
	}
}
