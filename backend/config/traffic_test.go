package config

import "testing"

func TestTrafficNetworkValidation(t *testing.T) {
	for _, cidr := range []string{"0.0.0.0/0", "192.0.2.1/24", "bad", "::ffff:192.0.2.0/120"} {
		if _, err := (Team{IP: "192.0.2.1", TrafficCIDRs: []string{cidr}}).TrafficNetworks(); err == nil {
			t.Fatalf("accepted %q", cidr)
		}
	}
	teams := []Team{{IP: "192.0.2.1", Name: "One", TrafficCIDRs: []string{"192.0.2.0/24"}}, {IP: "192.0.2.2", Name: "Two"}}
	if err := validateTeams(teams); err == nil {
		t.Fatal("accepted overlapping team networks")
	}
	teams[0].TrafficCIDRs = []string{"198.51.100.0/24"}
	if err := validateTeams(teams); err != nil {
		t.Fatal(err)
	}
}
