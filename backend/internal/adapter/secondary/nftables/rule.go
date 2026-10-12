package nftables

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/netip"
	"strings"
)

const tableName = "forcad_monitoring"

// RenderRules returns an nftables script that creates only the monitoring table.
func RenderRules(inventory Inventory) (string, error) {
	if err := validateInventory(inventory); err != nil {
		return "", err
	}
	fingerprint, err := inventoryFingerprint(inventory)
	if err != nil {
		return "", err
	}
	var script strings.Builder
	// The create operation makes a repeated nft -f apply fail before adding
	// another copy of the owned table's chains, counters, or rules.
	fmt.Fprintf(&script, "create table inet %s\n", tableName)
	fmt.Fprintf(&script, "add chain inet %s forward { type filter hook forward priority -10; policy accept; }\n", tableName)
	comment := inventoryComment(fingerprint)
	for _, team := range inventory.Teams {
		fmt.Fprintf(&script, "add counter inet %s %s { packets 0 bytes 0; comment \"%s\"; }\n", tableName, counterName(team.TeamIP, "rx"), comment)
		fmt.Fprintf(&script, "add counter inet %s %s { packets 0 bytes 0; comment \"%s\"; }\n", tableName, counterName(team.TeamIP, "tx"), comment)
	}
	for _, team := range inventory.Teams {
		for _, value := range team.Networks {
			prefix, err := netip.ParsePrefix(value)
			if err != nil {
				return "", fmt.Errorf("nftables.RenderRules - invalid network in inventory")
			}
			family := "ip"
			if prefix.Addr().Is6() {
				family = "ip6"
			}
			fmt.Fprintf(&script, "add rule inet %s forward %s daddr %s counter name %s\n", tableName, family, value, counterName(team.TeamIP, "rx"))
			fmt.Fprintf(&script, "add rule inet %s forward %s saddr %s counter name %s\n", tableName, family, value, counterName(team.TeamIP, "tx"))
		}
	}
	return script.String(), nil
}

func counterName(teamIP, direction string) string {
	digest := sha256.Sum256([]byte(teamIP))
	return "team_" + hex.EncodeToString(digest[:]) + "_" + direction
}
