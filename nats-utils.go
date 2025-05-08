package natsdistributedlock

import (
	"context"
	"os"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

func EstablishNATSConnectivity(ctx context.Context, natsURL, natsCreds, name string) *nats.Conn {
	if natsCreds == "" {
		natsCreds = os.Getenv("NATS_CREDS")
	}
	opts := []nats.Option{nats.Name(name)}
	switch {
	case natsCreds != "":
		opts = append(opts, nats.UserCredentials(natsCreds))
	case os.Getenv("NATS_NKEY") != "":
		opt, err := nats.NkeyOptionFromSeed(os.Getenv("NATS_NKEY"))
		if err == nil {
			opts = append(opts, opt)
		}
	case os.Getenv("NATS_USER") != "" && os.Getenv("NATS_PASSWORD") != "":
		opts = append(opts, nats.UserInfo(os.Getenv("NATS_USER"), os.Getenv("NATS_PASSWORD")))
	case os.Getenv("NATS_TOKEN") != "":
		opts = append(opts, nats.Token(os.Getenv("NATS_TOKEN")))
	}
	for {
		nc, err := nats.Connect(natsURL, opts...)
		if err == nil {
			return nc
		}
		log.Error().Err(err).Msg("error connecting to NATS, will try again in 5 seconds")
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(time.Second * 5):
		}
	}
}
