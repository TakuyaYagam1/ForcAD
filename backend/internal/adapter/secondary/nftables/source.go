package nftables

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
)

type sourceSample struct {
	sourceIP string
	teamIP   string
	value    counterValue
}

func parseNftSources(data []byte, inventory Inventory) ([]sourceSample, bool, error) {
	const location = "nftables.parseNftSources"
	if len(data) > maxSourceOutputBytes {
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
		return nil, false, fmt.Errorf("%s - invalid nft set response", location)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, false, fmt.Errorf("%s - nft response must contain one JSON value", location)
	}
	fingerprint, err := inventoryFingerprint(inventory)
	if err != nil {
		return nil, false, err
	}
	knownTeams := make(map[string]struct{}, len(inventory.Teams))
	for _, team := range inventory.Teams {
		knownTeams[team.TeamIP] = struct{}{}
	}
	samples, complete, err := decodeSourceItems(response.Nftables, inventoryComment(fingerprint), knownTeams)
	if err != nil {
		return nil, false, err
	}
	return samples, complete, nil
}

func decodeSourceItems(items []json.RawMessage, comment string, knownTeams map[string]struct{}) ([]sourceSample, bool, error) {
	const location = "nftables.decodeSourceItems"
	seenSets := make(map[string]struct{}, 2)
	seenSources := make(map[[2]string]struct{})
	var samples []sourceSample
	for _, raw := range items {
		var item map[string]json.RawMessage
		if err := json.Unmarshal(raw, &item); err != nil || len(item) != 1 {
			return nil, false, fmt.Errorf("%s - invalid nft set item", location)
		}
		if _, isMeta := item["metainfo"]; isMeta {
			continue
		}
		rawSet, isSet := item["set"]
		if !isSet {
			return nil, false, fmt.Errorf("%s - unexpected nft item", location)
		}
		name, entries, err := decodeSourceSet(rawSet, comment, knownTeams)
		if err != nil {
			return nil, false, err
		}
		if _, duplicate := seenSets[name]; duplicate {
			return nil, false, fmt.Errorf("%s - duplicate source set", location)
		}
		seenSets[name] = struct{}{}
		for _, entry := range entries {
			key := [2]string{entry.sourceIP, entry.teamIP}
			if _, duplicate := seenSources[key]; duplicate {
				return nil, false, fmt.Errorf("%s - duplicate source pair", location)
			}
			seenSources[key] = struct{}{}
			samples = append(samples, entry)
		}
	}
	return samples, len(seenSets) == 2, nil
}

func decodeSourceSet(raw json.RawMessage, comment string, knownTeams map[string]struct{}) (string, []sourceSample, error) {
	const location = "nftables.decodeSourceSet"
	var set struct {
		Family  string            `json:"family"`
		Table   string            `json:"table"`
		Name    string            `json:"name"`
		Comment string            `json:"comment"`
		Type    []string          `json:"type"`
		Elem    []json.RawMessage `json:"elem"`
	}
	if err := json.Unmarshal(raw, &set); err != nil || set.Family != "inet" || set.Table != tableName || set.Comment != comment {
		return "", nil, fmt.Errorf("%s - source set or inventory does not match", location)
	}
	var addressType string
	switch set.Name {
	case "sources_v4":
		addressType = "ipv4_addr"
	case "sources_v6":
		addressType = "ipv6_addr"
	default:
		return "", nil, fmt.Errorf("%s - unexpected source set", location)
	}
	if len(set.Type) != 2 || set.Type[0] != addressType || set.Type[1] != addressType || len(set.Elem) > sourceSetSize {
		return "", nil, fmt.Errorf("%s - invalid source set shape", location)
	}
	samples := make([]sourceSample, 0, len(set.Elem))
	for _, rawElement := range set.Elem {
		sample, err := decodeSourceElement(rawElement, addressType == "ipv4_addr", knownTeams)
		if err != nil {
			return "", nil, err
		}
		samples = append(samples, sample)
	}
	return set.Name, samples, nil
}

func decodeSourceElement(raw json.RawMessage, ipv4 bool, knownTeams map[string]struct{}) (sourceSample, error) {
	const location = "nftables.decodeSourceElement"
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
	if err := decoder.Decode(&item); err != nil || len(item.Elem.Val.Concat) != 2 {
		return sourceSample{}, fmt.Errorf("%s - invalid source element", location)
	}
	source, sourceErr := netip.ParseAddr(item.Elem.Val.Concat[0])
	target, targetErr := netip.ParseAddr(item.Elem.Val.Concat[1])
	if sourceErr != nil || targetErr != nil || source.Is4() != ipv4 || target.Is4() != ipv4 || source.String() != item.Elem.Val.Concat[0] || target.String() != item.Elem.Val.Concat[1] {
		return sourceSample{}, fmt.Errorf("%s - invalid source or target IP", location)
	}
	if _, known := knownTeams[target.String()]; !known {
		return sourceSample{}, fmt.Errorf("%s - unknown destination team", location)
	}
	packets, packetErr := parseCounterNumber(item.Elem.Counter.Packets)
	bytes, byteErr := parseCounterNumber(item.Elem.Counter.Bytes)
	if packetErr != nil || byteErr != nil {
		return sourceSample{}, fmt.Errorf("%s - invalid source counter", location)
	}
	return sourceSample{sourceIP: source.String(), teamIP: target.String(), value: counterValue{packets: packets, bytes: bytes}}, nil
}
