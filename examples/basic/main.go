package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	ndl "github.com/arunsworld/nats-distributed-lock"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	zerolog.SetGlobalLevel(zerolog.InfoLevel)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx); err != nil {
		log.Fatal().Err(err).Msg("error running application")
	}
}

func run(ctx context.Context) error {
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = "nats://localhost:4222"
	}

	nc := ndl.EstablishNATSConnectivity(ctx, natsURL, "", "example-basic")
	if nc == nil {
		return nil
	}
	defer nc.Drain()
	log.Info().Msg("connected to NATS")

	js, err := jetstream.New(nc)
	if err != nil {
		return err
	}

	dlock, err := ndl.NewNatsDistributedLock(js, "example-basic", ndl.WithTTL(5*time.Second))
	if err != nil {
		return err
	}

	doneWithJobA := dlock.DoWorkWhenElected("job-A", func(ctx context.Context) {
		log.Info().Msg("starting job-A work")
		<-ctx.Done()
		log.Info().Msg("stopping job-A work")
	})
	defer doneWithJobA.Close()

	doneWithJobB := dlock.DoWorkWhenElected("job-B", func(ctx context.Context) {
		log.Info().Msg("starting job-B work")
		<-ctx.Done()
		log.Info().Msg("stopping job-B work")
	})
	defer doneWithJobB.Close()

	<-ctx.Done()

	return nil
}
