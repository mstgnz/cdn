// Command hostwatch mails an operator when the host's disk, inode or memory
// usage crosses a threshold. See docs/deployment.md, "Host monitoring".
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mstgnz/cdn/pkg/hostwatch"
	"github.com/rs/zerolog"
)

func main() {
	zerolog.TimeFieldFormat = time.RFC3339
	log := zerolog.New(os.Stdout).With().Timestamp().Str("component", "hostwatch").Logger()

	cfg, err := hostwatch.LoadConfig(os.Getenv)
	if err != nil {
		log.Fatal().Err(err).Msg("invalid configuration")
	}
	if cfg.Hostname == "" {
		cfg.Hostname = hostwatch.ReadHostname(cfg.RootFS)
	}

	var hb hostwatch.Heartbeat
	if cfg.HeartbeatURL != "" {
		push, err := hostwatch.NewPushHeartbeat(cfg.HeartbeatURL)
		if err != nil {
			log.Fatal().Err(err).Msg("invalid heartbeat URL")
		}
		hb = push
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	mon := hostwatch.NewMonitor(cfg, hostwatch.NewHostCollector(cfg), hostwatch.NewSMTPNotifier(cfg.SMTP), hb, log)
	if err := mon.Start(ctx); err != nil {
		log.Fatal().Err(err).Msg("startup failed")
	}
	log.Info().
		Str("host", cfg.Hostname).
		Dur("interval", cfg.Interval).
		Bool("heartbeat", hb != nil).
		Msg("monitoring")
	mon.Run(ctx, cfg.Interval)
	log.Info().Msg("stopped")
}
