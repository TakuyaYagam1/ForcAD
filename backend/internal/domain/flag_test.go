package domain

import (
	"errors"
	"math"
	"strings"
	"testing"
)

func TestFlagIsLiveIncludesLifetimeBoundary(t *testing.T) {
	tests := []struct {
		name      string
		flagRound int32
		lifetime  int32
		want      bool
	}{
		{name: "oldest live round", flagRound: 5, lifetime: 5, want: true},
		{name: "one round beyond lifetime", flagRound: 4, lifetime: 5, want: false},
		{name: "current round", flagRound: 10, lifetime: 5, want: true},
		{name: "future flag round", flagRound: 11, lifetime: 5, want: true},
		{name: "zero lifetime", flagRound: 10, lifetime: 0, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			flag := Flag{Round: test.flagRound}
			if got := flag.IsLive(10, test.lifetime); got != test.want {
				t.Fatalf("IsLive() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestCalculateRatingChangeForEqualScores(t *testing.T) {
	change, err := CalculateRatingChange(2500, 2500, 10, true)
	if err != nil {
		t.Fatal(err)
	}
	const halfScale = 79.05694150420949
	if math.Abs(change.Attacker-halfScale) > 1e-12 {
		t.Fatalf("attacker delta = %.14f, want %.14f", change.Attacker, halfScale)
	}
	if math.Abs(change.Victim+halfScale) > 1e-12 {
		t.Fatalf("victim delta = %.14f, want %.14f", change.Victim, -halfScale)
	}
}

func TestCalculateRatingChangeInflationAndVictimFloor(t *testing.T) {
	inflated, err := CalculateRatingChange(0, 10, 10, true)
	if err != nil {
		t.Fatal(err)
	}
	if inflated.Attacker <= 10 || inflated.Victim != -10 {
		t.Fatalf("inflation result = %+v, want attacker > 10 and victim = -10", inflated)
	}

	bounded, err := CalculateRatingChange(0, 10, 10, false)
	if err != nil {
		t.Fatal(err)
	}
	if bounded.Attacker != 10 || bounded.Victim != -10 {
		t.Fatalf("non-inflation result = %+v, want zero-sum 10/-10", bounded)
	}
}

func TestCalculateRatingChangeRejectsInvalidInputs(t *testing.T) {
	tests := []struct {
		name                       string
		attacker, victim, hardness float64
	}{
		{name: "negative attacker score", attacker: -1, victim: 0, hardness: 10},
		{name: "negative victim score", attacker: 0, victim: -1, hardness: 10},
		{name: "hardness at lower bound", attacker: 0, victim: 0, hardness: 1},
		{name: "not a number", attacker: math.NaN(), victim: 0, hardness: 10},
		{name: "infinite hardness", attacker: 0, victim: 0, hardness: math.Inf(1)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := CalculateRatingChange(test.attacker, test.victim, test.hardness, true)
			if !errors.Is(err, ErrInvalidGameConfig) {
				t.Fatalf("CalculateRatingChange() error = %v, want %v", err, ErrInvalidGameConfig)
			}
			if !strings.HasPrefix(err.Error(), "domain.CalculateRatingChange - ") {
				t.Fatalf("error is missing its source: %v", err)
			}
		})
	}
}
