package config

import (
	"net/url"
	"strings"
	"testing"
)

func TestNATSDefaultsAndCredentialEscaping(t *testing.T) {
	cfg, err := loadTest(map[string]string{
		"FORCAD_ROLE":   "worker",
		"NATS_USER":     "user:@",
		"NATS_PASSWORD": "p@ss/word?#$",
	})
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := url.Parse(cfg.NATSURL)
	if err != nil {
		t.Fatal(err)
	}
	password, _ := endpoint.User.Password()
	if endpoint.Scheme != "nats" || endpoint.Host != "nats:4222" || endpoint.User.Username() != "user:@" || password != "p@ss/word?#$" {
		t.Fatalf("NATS endpoint did not preserve credentials or default endpoint")
	}
	if cfg.NATSStream != "FORCAD_CHECKERS" || cfg.NATSSubject != "forcad.checker.jobs" || cfg.NATSConsumer != "forcad-workers" || cfg.NATSMaxAckPending != 1024 {
		t.Fatal("unexpected JetStream defaults")
	}
}

func TestNATSSettingsValidation(t *testing.T) {
	for _, test := range []struct {
		key, value string
	}{
		{"NATS_URL", "amqp://broker:5672"}, {"NATS_URL", "nats://broker:0"},
		{"NATS_URL", "nats://broker:65536"}, {"NATS_URL", "nats://broker/path"},
		{"NATS_STREAM", "invalid.stream"}, {"NATS_CONSUMER", "invalid.consumer"},
		{"NATS_SUBJECT", "forcad.*"}, {"NATS_SUBJECT", "forcad..jobs"},
		{"NATS_SUBJECT", ".forcad"}, {"NATS_SUBJECT", "forcad."},
		{"NATS_MAX_ACK_PENDING", "0"}, {"NATS_MAX_ACK_PENDING", "1000001"},
	} {
		t.Run(test.key+"/"+test.value, func(t *testing.T) {
			_, err := loadTest(map[string]string{
				"FORCAD_ROLE": "worker",
				test.key:      test.value,
			})
			if err == nil || !strings.Contains(err.Error(), test.key) {
				t.Fatalf("invalid %s was accepted: %v", test.key, err)
			}
		})
	}
	cfg, err := loadTest(map[string]string{
		"FORCAD_ROLE":          "worker",
		"NATS_URL":             "tls://broker.example:4222",
		"NATS_STREAM":          "CUSTOM_CHECKERS",
		"NATS_SUBJECT":         "custom.jobs",
		"NATS_CONSUMER":        "custom-workers",
		"NATS_MAX_ACK_PENDING": "80",
	})
	if err != nil || cfg.NATSURL != "tls://broker.example:4222" || cfg.NATSMaxAckPending != 80 {
		t.Fatalf("valid custom JetStream settings: %v", err)
	}
}
