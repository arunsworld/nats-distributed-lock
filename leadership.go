package natsdistributedlock

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/arunsworld/nursery"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/nats-io/nuid"
	"github.com/rs/zerolog/log"
)

// Job encapsulates work that should be done until the provided context is closed
type Job func(context.Context)

type (
	// NatsDistributedLock provides capability to encapsulate a job that should be done when elected
	NatsDistributedLock interface {
		// Multiple indepdendent jobs may be defined differentiated by their name
		// Returns a Closer whos Close method must be called when quitting to cleanup resources
		DoWorkWhenElected(name string, job Job) io.Closer
	}

	Opts func(*natsDistributedLock) error
)

// Creates a NatsDistributedLock with a given namespace
func NewNatsDistributedLock(js jetstream.JetStream, name string, opts ...Opts) (NatsDistributedLock, error) {
	result := &natsDistributedLock{
		namespace:    name,
		instanceID:   nuid.Next(),
		ttl:          time.Second * 30,
		replicas:     1,
		refreshDelay: time.Second * 20,
		pollDelay:    time.Second * 15,
	}

	for _, opt := range opts {
		if err := opt(result); err != nil {
			return nil, err
		}
	}

	kv, err := js.CreateOrUpdateKeyValue(context.Background(), jetstream.KeyValueConfig{
		Bucket:      fmt.Sprintf("leadership-campaigns-%s", name),
		Description: fmt.Sprintf("bucket for leadership election for distributed lock %s", name),
		TTL:         result.ttl,
		Replicas:    result.replicas,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to create NATS KV Store: %w", err)
	}
	result.semaphore = kv

	log.Info().Str("namespace", name).Str("instance", result.instanceID).Dur("ttl", result.ttl).Dur("refreshDelay", result.refreshDelay).Dur("pollDelay", result.pollDelay).Int("replicas", result.replicas).Msg("nats distributed lock initialized")

	return result, nil
}

// ttl less than 5 seconds will not be accepted
func WithTTL(ttl time.Duration) Opts {
	return func(n *natsDistributedLock) error {
		if ttl < time.Second*5 {
			return fmt.Errorf("ttl must be at least 5 seconds")
		}
		n.ttl = ttl
		n.refreshDelay = n.ttl * 2 / 3
		n.pollDelay = n.ttl / 2
		log.Info().Dur("ttl", n.ttl).Dur("refreshDelay", n.refreshDelay).Dur("pollDelay", n.pollDelay).Msg("nats distributed lock: custom TTL configured")
		return nil
	}
}

func WithReplicas(replicas int) Opts {
	return func(n *natsDistributedLock) error {
		n.replicas = replicas
		log.Info().Int("replicas", n.replicas).Msg("nats distributed lock: custom replicas configured")
		return nil
	}
}

func WithInstanceID(id string) Opts {
	return func(n *natsDistributedLock) error {
		n.instanceID = id
		log.Info().Str("instanceID", n.instanceID).Msg("nats distributed lock: custom instance id configured")
		return nil
	}
}

type natsDistributedLock struct {
	namespace    string             // namespace within which the distributed lock election takes place
	instanceID   string             // unique ID to identify this instance; MUST be unique
	ttl          time.Duration      // KV TTL
	replicas     int                // KV replicas (for resiliency)
	refreshDelay time.Duration      // Time to wait before refreshing leadership
	pollDelay    time.Duration      // When not leader, how long to wait before polling for leadership
	semaphore    jetstream.KeyValue // internal - semaphore used to perform leadership election
}

func (n *natsDistributedLock) DoWorkWhenElected(name string, job Job) io.Closer {
	ctx, cancel := context.WithCancel(context.Background())

	dl := &campaign{
		namespace:    n.namespace,
		semaphore:    n.semaphore,
		name:         name,
		instanceID:   n.instanceID,
		job:          job,
		refreshDelay: n.refreshDelay,
		pollDelay:    n.pollDelay,
		stopRunning:  cancel,
		running:      make(chan struct{}),
	}
	go dl.run(ctx)
	return dl
}

type campaign struct {
	namespace    string
	semaphore    jetstream.KeyValue
	name         string
	instanceID   string
	job          Job
	refreshDelay time.Duration // Time to wait before refreshing leadership
	pollDelay    time.Duration // When not leader, how long to wait before polling for leadership
	// internal
	stopRunning context.CancelFunc
	running     chan (struct{})
}

func (c *campaign) Close() error {
	c.stopRunning()
	<-c.running
	log.Info().Str("namespace", c.namespace).Str("campaign", c.name).Str("instance", c.instanceID).Msg("exited leadership campaign")
	return nil
}

func (c *campaign) run(ctx context.Context) {
	log.Info().Str("namespace", c.namespace).Str("campaign", c.name).Str("instance", c.instanceID).Msg("running leadership campaign")
	defer close(c.running)

	for {
		_, err := c.semaphore.Create(ctx, c.name, []byte(c.instanceID))
		switch {
		case err == nil:
			log.Info().Str("namespace", c.namespace).Str("campaign", c.name).Str("instance", c.instanceID).Msg("elected as leader")
			c.doWorkAndKeepAlive(ctx)
			select {
			case <-ctx.Done():
				log.Debug().Str("namespace", c.namespace).Str("campaign", c.name).Str("instance", c.instanceID).Msg("terminating leadership campaign")
				return
			default:
			}
		case errors.Is(err, jetstream.ErrKeyExists):
			log.Debug().Str("namespace", c.namespace).Str("campaign", c.name).Str("instance", c.instanceID).Msg("someone else is leader")
		default:
			log.Error().Str("namespace", c.namespace).Err(err).Str("campaign", c.name).Str("instance", c.instanceID).Msg("error in leadership campaign")
		}

		select {
		case <-ctx.Done():
			log.Debug().Str("namespace", c.namespace).Str("campaign", c.name).Str("instance", c.instanceID).Msg("terminating leadership campaign")
			return
		case <-time.After(c.pollDelay):
		}
	}
}

func (c *campaign) doWorkAndKeepAlive(ctx context.Context) {
	ctx, stopWork := context.WithCancel(ctx)
	workStopped := make(chan struct{})

	nursery.RunConcurrently(
		func(context.Context, chan error) {
			c.job(ctx)
			close(workStopped)
			log.Info().Str("namespace", c.namespace).Str("campaign", c.name).Str("instance", c.instanceID).Msg("leadership ended")
		},
		func(context.Context, chan error) {
			for {
				select {
				case <-workStopped:
					c.quitLeadership()
					return
				case <-time.After(c.refreshDelay):
					if err := c.refreshLeadership(); err != nil {
						log.Error().Err(err).Str("namespace", c.namespace).Str("campaign", c.name).Str("instance", c.instanceID).Msg("unable to refresh leadership")
						stopWork()
						<-workStopped
						return
					}
					log.Debug().Str("namespace", c.namespace).Str("campaign", c.name).Str("instance", c.instanceID).Msg("leadership refreshed")
				}
			}
		},
	)
}

func (c *campaign) refreshLeadership() error {
	kve, err := c.semaphore.Get(context.Background(), c.name)
	if err != nil {
		return fmt.Errorf("failed to get campaign: %w", err)
	}
	currentLeader := string(kve.Value())
	if currentLeader != c.instanceID {
		return fmt.Errorf("no longer leader, %s is leader instead", currentLeader)
	}
	if _, err := c.semaphore.Update(context.Background(), c.name, []byte(c.instanceID), kve.Revision()); err != nil {
		return fmt.Errorf("failed to update campaign: %w", err)
	}
	return nil
}

func (c *campaign) quitLeadership() {
	kve, err := c.semaphore.Get(context.Background(), c.name)
	if err != nil {
		log.Error().Err(err).Str("namespace", c.namespace).Str("campaign", c.name).Str("instance", c.instanceID).Msg("failed to get campaign while quitting leadership")
		return
	}
	currentLeader := string(kve.Value())
	if currentLeader != c.instanceID {
		log.Error().Str("namespace", c.namespace).Str("campaign", c.name).Str("instance", c.instanceID).Msg(fmt.Sprintf("no longer leader when quitting leadership, %s is leader instead", currentLeader))
		return
	}
	if err := c.semaphore.Purge(context.Background(), c.name, jetstream.LastRevision(kve.Revision())); err != nil {
		log.Error().Err(err).Str("namespace", c.namespace).Str("campaign", c.name).Str("instance", c.instanceID).Msg("failed to purge campaign while quitting leadership")
		return
	}
	log.Debug().Str("namespace", c.namespace).Str("campaign", c.name).Str("instance", c.instanceID).Msg("leadership quit")
}
