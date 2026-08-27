package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/snowaner-ustc/ResourceHub/server/internal/alerts"
	"github.com/snowaner-ustc/ResourceHub/server/internal/api"
	"github.com/snowaner-ustc/ResourceHub/server/internal/store"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	dbPath := flag.String("db", "resourcehub.db", "sqlite database path")
	offlineAfter := flag.Duration("offline-after", 45*time.Second, "mark host offline after no heartbeat")
	flag.Parse()

	st, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	ev := alerts.New(st, alerts.DefaultConfig())
	srv := api.New(st, ev, *offlineAfter)

	stop := make(chan struct{})
	go srv.RunOfflineChecker(stop)

	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("ResourceHub server listening on %s", *addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	close(stop)
	_ = httpServer.Close()
}
