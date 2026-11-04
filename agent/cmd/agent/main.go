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

const agentVersion = "0.2.0"

func main() {
	serverURL := flag.String("server", "http://127.0.0.1:8080", "ResourceHub server URL")
	name := flag.String("name", "", "display name (defaults to hostname)")
	interval := flag.Duration("interval", 10*time.Second, "metrics report interval")
	procInterval := flag.Duration("proc-interval", 30*time.Second, "process scan interval (mid-path)")
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
		resp, err := client.Register(*name, hostname, agentVersion)
		if err != nil {
			log.Fatalf("register: %v", err)
		}
		if err := reporter.SaveToken(*tokenFile, resp.Token); err != nil {
			log.Fatalf("save token: %v", err)
		}
		log.Printf("registered host_id=%s", resp.HostID)
	}

	col := collector.New()
	// Prime CPU delta baseline and first process sample (Top CPU needs a second sample later).
	if _, err := col.Collect(); err != nil {
		log.Printf("initial collect warning: %v", err)
	}
	proc := col.CollectProcesses()
	log.Printf("initial process scan: total=%d zombie=%d duration=%dms partial=%v",
		proc.Summary.Total, proc.Summary.Zombie, proc.Summary.CollectDurationMs, proc.Summary.Partial)

	ticker := time.NewTicker(*interval)
	defer ticker.Stop()
	procTicker := time.NewTicker(*procInterval)
	defer procTicker.Stop()

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
		log.Printf("reported cpu=%.1f%% mem=%.1f%% disks=%d procs=%d zombies=%d duration=%dms",
			snap.CPU.UsagePercent, snap.Memory.UsedPercent, len(snap.Disks), total, zombie, snap.CollectDurationMs)
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
		case <-sig:
			return
		}
	}
}
