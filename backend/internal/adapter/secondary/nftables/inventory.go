package nftables

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"sort"

	"github.com/TakuyaYagam1/ForcAD/config"
)

const (
	maxInventoryTeams     = 1024
	maxNetworksPerTeam    = 32
	maxInventoryJSONBytes = 16 << 20
)

// Inventory contains only addresses and networks needed by the exporter.
type Inventory struct {
	Teams []InventoryTeam `json:"teams"`
}

// InventoryTeam identifies the networks counted for one team.
type InventoryTeam struct {
	TeamIP   string   `json:"team_ip"`
	Networks []string `json:"networks"`
}

// InventoryFromDocument extracts the network inventory without copying team secrets.
func InventoryFromDocument(document config.Document) (Inventory, error) {
	const location = "nftables.InventoryFromDocument"
	if len(document.Teams) > maxInventoryTeams {
		return Inventory{}, fmt.Errorf("%s - at most %d teams are allowed", location, maxInventoryTeams)
	}

	inventory := Inventory{
		Teams: make([]InventoryTeam, 0, len(document.Teams)),
	}
	for index, team := range document.Teams {
		address, err := netip.ParseAddr(team.IP)
		if err != nil || !address.IsValid() || address.Zone() != "" {
			return Inventory{}, fmt.Errorf("%s - teams[%d] has an invalid IP address", location, index)
		}
		address = address.Unmap()

		networks, err := team.TrafficNetworks()
		if err != nil {
			return Inventory{}, fmt.Errorf("%s - teams[%d] has invalid traffic networks: %w", location, index, err)
		}
		item := InventoryTeam{
			TeamIP:   address.String(),
			Networks: make([]string, 0, len(networks)),
		}
		for _, network := range networks {
			item.Networks = append(item.Networks, network.String())
		}
		inventory.Teams = append(inventory.Teams, item)
	}
	if err := validateInventory(inventory); err != nil {
		return Inventory{}, fmt.Errorf("%s - validate inventory: %w", location, err)
	}
	return inventory, nil
}

// ReadInventory loads and validates a sanitized JSON inventory.
func ReadInventory(path string) (Inventory, error) {
	const location = "nftables.ReadInventory"
	file, err := os.Open(path)
	if err != nil {
		return Inventory{}, fmt.Errorf("%s - open inventory: %w", location, err)
	}
	defer func() {
		_ = file.Close()
	}()
	data, err := io.ReadAll(io.LimitReader(file, maxInventoryJSONBytes+1))
	if err != nil {
		return Inventory{}, fmt.Errorf("%s - read inventory", location)
	}
	if len(data) > maxInventoryJSONBytes {
		return Inventory{}, fmt.Errorf("%s - inventory exceeds 16 MiB", location)
	}
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return Inventory{}, fmt.Errorf("%s - inventory contains invalid or duplicate JSON fields", location)
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var inventory Inventory
	if err := decoder.Decode(&inventory); err != nil {
		return Inventory{}, fmt.Errorf("%s - decode inventory", location)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Inventory{}, fmt.Errorf("%s - inventory must contain exactly one JSON value", location)
	}
	if err := validateInventory(inventory); err != nil {
		return Inventory{}, fmt.Errorf("%s - validate inventory: %w", location, err)
	}
	return inventory, nil
}

func validateInventory(inventory Inventory) error {
	const location = "nftables.validateInventory"
	if len(inventory.Teams) == 0 || len(inventory.Teams) > maxInventoryTeams {
		return fmt.Errorf("%s - team count must be between 1 and %d", location, maxInventoryTeams)
	}
	teamIPs := make(map[netip.Addr]struct{}, len(inventory.Teams))
	var prefixes []netip.Prefix
	for teamIndex, team := range inventory.Teams {
		address, err := inventoryTeamAddress(team.TeamIP, teamIndex)
		if err != nil {
			return fmt.Errorf("%s - validate team address: %w", location, err)
		}
		if _, exists := teamIPs[address]; exists {
			return fmt.Errorf("%s - duplicate team_ip at teams[%d]", location, teamIndex)
		}
		teamIPs[address] = struct{}{}
		networks, err := inventoryTeamNetworks(team, teamIndex)
		if err != nil {
			return fmt.Errorf("%s - validate team networks: %w", location, err)
		}
		prefixes = append(prefixes, networks...)
	}
	if err := validateNetworkOverlap(prefixes); err != nil {
		return fmt.Errorf("%s - validate network overlap: %w", location, err)
	}
	return nil
}

func inventoryTeamAddress(value string, teamIndex int) (netip.Addr, error) {
	const location = "nftables.inventoryTeamAddress"
	address, err := netip.ParseAddr(value)
	if err != nil || !address.IsValid() || address.Zone() != "" || address.Is4In6() || address.String() != value {
		return netip.Addr{}, fmt.Errorf("%s - teams[%d] has a non-canonical team_ip", location, teamIndex)
	}
	return address, nil
}

func inventoryTeamNetworks(team InventoryTeam, teamIndex int) ([]netip.Prefix, error) {
	const location = "nftables.inventoryTeamNetworks"
	if len(team.Networks) == 0 || len(team.Networks) > maxNetworksPerTeam {
		return nil, fmt.Errorf("%s - teams[%d] network count must be between 1 and %d", location, teamIndex, maxNetworksPerTeam)
	}
	prefixes := make([]netip.Prefix, 0, len(team.Networks))
	for networkIndex, value := range team.Networks {
		prefix, err := netip.ParsePrefix(value)
		if err != nil || prefix.Bits() == 0 || prefix.Addr().Is4In6() || prefix != prefix.Masked() || prefix.String() != value {
			return nil, fmt.Errorf("%s - teams[%d].networks[%d] is not a canonical network", location, teamIndex, networkIndex)
		}
		prefixes = append(prefixes, prefix)
	}
	return prefixes, nil
}

func validateNetworkOverlap(prefixes []netip.Prefix) error {
	const location = "nftables.validateNetworkOverlap"
	sort.Slice(prefixes, func(left, right int) bool {
		leftPrefix, rightPrefix := prefixes[left], prefixes[right]
		if leftPrefix.Addr().Is4() != rightPrefix.Addr().Is4() {
			return leftPrefix.Addr().Is4()
		}
		if comparison := leftPrefix.Addr().Compare(rightPrefix.Addr()); comparison != 0 {
			return comparison < 0
		}
		return leftPrefix.Bits() < rightPrefix.Bits()
	})
	for index := 1; index < len(prefixes); index++ {
		if prefixesOverlap(prefixes[index-1], prefixes[index]) {
			return fmt.Errorf("%s - traffic networks overlap", location)
		}
	}
	return nil
}

func prefixesOverlap(left, right netip.Prefix) bool {
	if left.Addr().Is4() != right.Addr().Is4() {
		return false
	}
	return left.Contains(right.Addr()) || right.Contains(left.Addr())
}

func inventoryFingerprint(inventory Inventory) (string, error) {
	const location = "nftables.inventoryFingerprint"
	if err := validateInventory(inventory); err != nil {
		return "", fmt.Errorf("%s - validate inventory: %w", location, err)
	}
	teams := append([]InventoryTeam(nil), inventory.Teams...)
	sort.Slice(teams, func(left, right int) bool {
		return teams[left].TeamIP < teams[right].TeamIP
	})
	var canonical bytes.Buffer
	for _, team := range teams {
		canonical.WriteString(team.TeamIP)
		canonical.WriteByte(0)
		networks := append([]string(nil), team.Networks...)
		sort.Strings(networks)
		for _, network := range networks {
			canonical.WriteString(network)
			canonical.WriteByte(0)
		}
		canonical.WriteByte('\n')
	}
	digest := sha256.Sum256(canonical.Bytes())
	return hex.EncodeToString(digest[:]), nil
}

func inventoryComment(fingerprint string) string {
	return "forcad_inventory_sha256_" + fingerprint
}

func rejectDuplicateJSONKeys(data []byte) error {
	const location = "nftables.rejectDuplicateJSONKeys"
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := consumeJSONValue(decoder); err != nil {
		return fmt.Errorf("%s - read JSON value: %w", location, err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%s - trailing JSON data", location)
	}
	return nil
}

func consumeJSONValue(decoder *json.Decoder) error {
	const location = "nftables.consumeJSONValue"
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("%s - read JSON token: %w", location, err)
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		if err := consumeJSONObject(decoder); err != nil {
			return fmt.Errorf("%s - read JSON object: %w", location, err)
		}
		return nil
	case '[':
		if err := consumeJSONArray(decoder); err != nil {
			return fmt.Errorf("%s - read JSON array: %w", location, err)
		}
		return nil
	default:
		return fmt.Errorf("%s - unexpected JSON delimiter", location)
	}
}

func consumeJSONObject(decoder *json.Decoder) error {
	const location = "nftables.consumeJSONObject"
	keys := make(map[string]struct{})
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("%s - read object key: %w", location, err)
		}
		key, ok := keyToken.(string)
		if !ok {
			return fmt.Errorf("%s - invalid JSON object key", location)
		}
		if _, exists := keys[key]; exists {
			return fmt.Errorf("%s - duplicate JSON object key", location)
		}
		keys[key] = struct{}{}
		if err := consumeJSONValue(decoder); err != nil {
			return fmt.Errorf("%s - read object value: %w", location, err)
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return fmt.Errorf("%s - invalid JSON object", location)
	}
	return nil
}

func consumeJSONArray(decoder *json.Decoder) error {
	const location = "nftables.consumeJSONArray"
	for decoder.More() {
		if err := consumeJSONValue(decoder); err != nil {
			return fmt.Errorf("%s - read array value: %w", location, err)
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim(']') {
		return fmt.Errorf("%s - invalid JSON array", location)
	}
	return nil
}
