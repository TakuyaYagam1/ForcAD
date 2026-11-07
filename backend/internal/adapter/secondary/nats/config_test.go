package natsbroker

import (
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

func compatibleConsumer(b *Broker) jetstream.ConsumerConfig {
	return jetstream.ConsumerConfig{
		Durable:           b.config.Consumer,
		FilterSubject:     b.config.Subject,
		AckPolicy:         jetstream.AckExplicitPolicy,
		AckWait:           ackWait,
		MaxDeliver:        -1,
		MaxAckPending:     b.config.MaxAckPending,
		MaxWaiting:        b.config.MaxAckPending,
		MaxRequestBatch:   1,
		MaxRequestExpires: 2 * fetchWait,
	}
}

func TestExistingConsumerCannotSkipBacklogOrRejectWorkerPulls(t *testing.T) {
	b := unitBroker()
	base := compatibleConsumer(b)
	if err := b.validateConsumer(base); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*jetstream.ConsumerConfig){
		"new messages only": func(c *jetstream.ConsumerConfig) {
			c.DeliverPolicy = jetstream.DeliverNewPolicy
		},
		"original replay rate": func(c *jetstream.ConsumerConfig) {
			c.ReplayPolicy = jetstream.ReplayOriginalPolicy
		},
		"pull duration too short": func(c *jetstream.ConsumerConfig) {
			c.MaxRequestExpires = fetchWait - time.Nanosecond
		},
		"too few fleet pull requests": func(c *jetstream.ConsumerConfig) {
			c.MaxWaiting = b.config.MaxAckPending - 1
		},
		"body omitted": func(c *jetstream.ConsumerConfig) {
			c.HeadersOnly = true
		},
		"byte budget": func(c *jetstream.ConsumerConfig) {
			c.MaxRequestMaxBytes = 1
		},
		"redelivery exhaustion": func(c *jetstream.ConsumerConfig) {
			c.MaxDeliver = 1
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			config := base
			change(&config)
			if b.validateConsumer(config) == nil {
				t.Fatal("incompatible consumer accepted")
			}
		})
	}
	base.MaxRequestExpires = 0
	base.MaxRequestBatch = 0
	if err := b.validateConsumer(base); err != nil {
		t.Fatalf("unrestricted pull limits rejected: %v", err)
	}
}
