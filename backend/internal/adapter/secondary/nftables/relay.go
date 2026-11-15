package nftables

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
)

const (
	routerTableName = "forcad_router"
	relaySetName    = "relay_attempts"
)

type relaySample struct {
	sourceTeamIP string
	teamIP       string
	packets      uint64
}

func parseNftRelayAttempts(data []byte, inventory Inventory) ([]relaySample, bool, error) {
	const location = "nftables.parseNftRelayAttempts"
	if len(data) > maxRelayOutputBytes {
		return nil, false, fmt.Errorf("%s - nft output exceeds 2 MiB", location)
	}
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return nil, false, fmt.Errorf("%s - invalid or duplicate nft JSON fields", location)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var response struct {
		Nftables []json.RawMessage `json:"nftables"`
	}
	if err := decoder.Decode(&response); err != nil || response.Nftables == nil {
		return nil, false, fmt.Errorf("%s - invalid nft relay set response", location)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, false, fmt.Errorf("%s - nft response must contain one JSON value", location)
	}

	fingerprint, err := inventoryFingerprint(inventory)
	if err != nil {
		return nil, false, fmt.Errorf("%s - fingerprint relay inventory: %w", location, err)
	}
	knownTeams := make(map[string]struct{}, len(inventory.Teams))
	for _, team := range inventory.Teams {
		knownTeams[team.TeamIP] = struct{}{}
	}

	var samples []relaySample
	seenSet := false
	for _, raw := range response.Nftables {
		var item map[string]json.RawMessage
		if err := json.Unmarshal(raw, &item); err != nil || len(item) != 1 {
			return nil, false, fmt.Errorf("%s - invalid nft relay item", location)
		}
		if _, isMeta := item["metainfo"]; isMeta {
			continue
		}
		rawSet, isSet := item["set"]
		if !isSet || seenSet {
			return nil, false, fmt.Errorf("%s - unexpected or duplicate relay set", location)
		}
		seenSet = true
		samples, err = decodeRelaySet(rawSet, inventoryComment(fingerprint), knownTeams)
		if err != nil {
			return nil, false, fmt.Errorf("%s - decode relay set: %w", location, err)
		}
	}
	return samples, seenSet, nil
}

func decodeRelaySet(raw json.RawMessage, comment string, knownTeams map[string]struct{}) ([]relaySample, error) {
	const location = "nftables.decodeRelaySet"
	var set struct {
		Family  string            `json:"family"`
		Table   string            `json:"table"`
		Name    string            `json:"name"`
		Comment string            `json:"comment"`
		Type    []string          `json:"type"`
		Elem    []json.RawMessage `json:"elem"`
	}
	if err := json.Unmarshal(raw, &set); err != nil ||
		set.Family != "inet" || set.Table != routerTableName || set.Name != relaySetName ||
		set.Comment != comment || len(set.Type) != 2 ||
		set.Type[0] != "ipv4_addr" || set.Type[1] != "ipv4_addr" {
		return nil, fmt.Errorf("%s - relay set or inventory does not match", location)
	}
	pairLimit := len(knownTeams) * len(knownTeams)
	if len(set.Elem) > pairLimit {
		return nil, fmt.Errorf("%s - relay set exceeds configured team pairs", location)
	}
	samples := make([]relaySample, 0, len(set.Elem))
	seenPairs := make(map[[2]string]struct{}, len(set.Elem))
	for _, rawElement := range set.Elem {
		sample, err := decodeRelayElement(rawElement, knownTeams)
		if err != nil {
			return nil, fmt.Errorf("%s - decode relay element: %w", location, err)
		}
		pair := [2]string{sample.sourceTeamIP, sample.teamIP}
		if _, duplicate := seenPairs[pair]; duplicate {
			return nil, fmt.Errorf("%s - duplicate relay pair", location)
		}
		seenPairs[pair] = struct{}{}
		samples = append(samples, sample)
	}
	return samples, nil
}

func decodeRelayElement(raw json.RawMessage, knownTeams map[string]struct{}) (relaySample, error) {
	const location = "nftables.decodeRelayElement"
	var item struct {
		Elem struct {
			Val struct {
				Concat []string `json:"concat"`
			} `json:"val"`
			Counter struct {
				Packets json.Number `json:"packets"`
				Bytes   json.Number `json:"bytes"`
			} `json:"counter"`
		} `json:"elem"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&item); err != nil || len(item.Elem.Val.Concat) != 2 {
		return relaySample{}, fmt.Errorf("%s - invalid relay element", location)
	}
	source, sourceErr := netip.ParseAddr(item.Elem.Val.Concat[0])
	target, targetErr := netip.ParseAddr(item.Elem.Val.Concat[1])
	if sourceErr != nil || targetErr != nil || !source.Is4() || !target.Is4() ||
		source.String() != item.Elem.Val.Concat[0] || target.String() != item.Elem.Val.Concat[1] {
		return relaySample{}, fmt.Errorf("%s - invalid relay source or target IP", location)
	}
	if _, known := knownTeams[source.String()]; !known {
		return relaySample{}, fmt.Errorf("%s - unknown source team", location)
	}
	if _, known := knownTeams[target.String()]; !known {
		return relaySample{}, fmt.Errorf("%s - unknown target team", location)
	}
	packets, packetErr := parseCounterNumber(item.Elem.Counter.Packets)
	_, byteErr := parseCounterNumber(item.Elem.Counter.Bytes)
	if packetErr != nil || byteErr != nil {
		return relaySample{}, fmt.Errorf("%s - invalid relay counter", location)
	}
	return relaySample{
		sourceTeamIP: source.String(),
		teamIP:       target.String(),
		packets:      packets,
	}, nil
}
