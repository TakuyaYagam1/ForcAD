package response

import (
	"encoding/json"
	"testing"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func TestPublicHistoryRoundMetrics(t *testing.T) {
	zeroFlags := int64(0)
	lostFlags := int64(3)
	capturedFlags := int64(2)
	zeroFP := 0.0
	opponentFP := 4.75
	gainedFP := 1.5
	tests := []struct {
		name              string
		flagsLost         *int64
		opponentFP        *float64
		flagsCaptured     *int64
		fpGained          *float64
		wantFlagsLost     string
		wantOpponentFP    string
		wantFlagsCaptured string
		wantFPGained      string
	}{
		{
			name:              "known zeros",
			flagsLost:         &zeroFlags,
			opponentFP:        &zeroFP,
			flagsCaptured:     &zeroFlags,
			fpGained:          &zeroFP,
			wantFlagsLost:     "0",
			wantOpponentFP:    "0",
			wantFlagsCaptured: "0",
			wantFPGained:      "0",
		},
		{
			name:              "unknown round data",
			wantFlagsLost:     "null",
			wantOpponentFP:    "null",
			wantFlagsCaptured: "null",
			wantFPGained:      "null",
		},
		{
			name:              "unknown opponent delta does not affect attacker totals",
			flagsLost:         &lostFlags,
			flagsCaptured:     &capturedFlags,
			fpGained:          &gainedFP,
			wantFlagsLost:     "3",
			wantOpponentFP:    "null",
			wantFlagsCaptured: "2",
			wantFPGained:      "1.5",
		},
		{
			name:              "unknown attacker delta does not affect victim totals",
			flagsLost:         &lostFlags,
			opponentFP:        &opponentFP,
			flagsCaptured:     &capturedFlags,
			wantFlagsLost:     "3",
			wantOpponentFP:    "4.75",
			wantFlagsCaptured: "2",
			wantFPGained:      "null",
		},
		{
			name:              "known credited totals",
			flagsLost:         &lostFlags,
			opponentFP:        &opponentFP,
			flagsCaptured:     &capturedFlags,
			fpGained:          &gainedFP,
			wantFlagsLost:     "3",
			wantOpponentFP:    "4.75",
			wantFlagsCaptured: "2",
			wantFPGained:      "1.5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := json.Marshal(PublicHistory(domain.PublicCell{
				RoundFlagsLost:     tt.flagsLost,
				RoundOpponentFP:    tt.opponentFP,
				RoundFlagsCaptured: tt.flagsCaptured,
				RoundFPGained:      tt.fpGained,
			}))
			if err != nil {
				t.Fatalf("marshal public history: %v", err)
			}

			var got map[string]json.RawMessage
			if err := json.Unmarshal(body, &got); err != nil {
				t.Fatalf("decode public history: %v", err)
			}
			if string(got["round_flags_lost"]) != tt.wantFlagsLost {
				t.Errorf("round_flags_lost = %s, want %s", got["round_flags_lost"], tt.wantFlagsLost)
			}
			if string(got["round_opponent_fp"]) != tt.wantOpponentFP {
				t.Errorf("round_opponent_fp = %s, want %s", got["round_opponent_fp"], tt.wantOpponentFP)
			}
			if string(got["round_flags_captured"]) != tt.wantFlagsCaptured {
				t.Errorf("round_flags_captured = %s, want %s", got["round_flags_captured"], tt.wantFlagsCaptured)
			}
			if string(got["round_fp_gained"]) != tt.wantFPGained {
				t.Errorf("round_fp_gained = %s, want %s", got["round_fp_gained"], tt.wantFPGained)
			}
		})
	}
}
