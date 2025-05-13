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

	jobACampaign := dlock.DoWorkWhenElected("job-A", func(ctx context.Context) {
		log.Info().Msg("starting job-A work")
		<-ctx.Done()
		log.Info().Msg("stopping job-A work")
	})
	defer jobACampaign.Close()

	jobBCampaign := dlock.DoWorkWhenElected("job-B", func(ctx context.Context) {
		log.Info().Msg("starting job-B work")
		<-ctx.Done()
		log.Info().Msg("stopping job-B work")
	})
	defer jobBCampaign.Close()

	go pollCampaigns(ctx, jobACampaign, jobBCampaign)

	<-ctx.Done()

	return nil
}

func pollCampaigns(ctx context.Context, campaigns ...ndl.Campaign) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			for _, c := range campaigns {
				name := c.Name()
				leader, err := c.CurrentLeader()
				if err != nil {
					log.Error().Err(err).Str("campaign", name).Msg("error getting current leader")
				} else {
					log.Info().Str("campaign", name).Str("instance", leader).Msg("current leader")
				}
			}
			select {
			case <-time.After(time.Second * 3):
			case <-ctx.Done():
				return
			}
		}
	}
}
