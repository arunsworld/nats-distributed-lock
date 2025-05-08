package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

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

	nc := ndl.EstablishNATSConnectivity(ctx, natsURL, "", "example-max-capacity")
	if nc == nil {
		return nil
	}
	defer nc.Drain()
	log.Info().Msg("connected to NATS")

	js, err := jetstream.New(nc)
	if err != nil {
		return err
	}

	dlock, err := ndl.NewNatsDistributedLock(js, "example-max-capacity")
	if err != nil {
		return err
	}

	c := capacity{
		maxCapacity: 1,
	}

	doneWithJobA := dlock.DoWorkWhenElected("job-A", func(ctx context.Context) {
		if err := c.doWork(); err != nil {
			log.Error().Err(err).Msg("error doing job-A work")
			return
		}
		defer c.releaseWork()

		log.Info().Msg("starting job-A work")
		<-ctx.Done()
		log.Info().Msg("stopping job-A work")
	})
	defer doneWithJobA.Close()

	doneWithJobB := dlock.DoWorkWhenElected("job-B", func(ctx context.Context) {
		if err := c.doWork(); err != nil {
			log.Error().Err(err).Msg("error doing job-B work")
			return
		}
		defer c.releaseWork()

		log.Info().Msg("starting job-B work")
		<-ctx.Done()
		log.Info().Msg("stopping job-B work")
	})
	defer doneWithJobB.Close()

	<-ctx.Done()

	return nil
}

type capacity struct {
	mu           sync.Mutex
	maxCapacity  int
	capacityUsed int
}

func (c *capacity) doWork() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.capacityUsed >= c.maxCapacity {
		return fmt.Errorf("already at capacity, cannot do more work")
	}

	c.capacityUsed++

	return nil
}

func (c *capacity) releaseWork() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.capacityUsed > 0 {
		c.capacityUsed--
	}
}
