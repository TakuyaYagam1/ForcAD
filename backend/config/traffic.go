package config

import (
	"fmt"
	"net/netip"
)

const maxTrafficNetworks = 32

// TrafficNetworks defaults to the service address. Explicit networks replace
// that default so the organizer controls the exact attribution boundary.
func (t Team) TrafficNetworks() ([]netip.Prefix, error) {
	const location = "config.Team.TrafficNetworks"
	if len(t.TrafficCIDRs) == 0 {
		address, err := netip.ParseAddr(t.IP)
		if err != nil {
			return nil, fmt.Errorf("%s - parse team address: %w", location, err)
		}
		address = address.Unmap()
		if address.Zone() != "" {
			return nil, fmt.Errorf("%s - scoped addresses cannot identify a team", location)
		}
		return []netip.Prefix{netip.PrefixFrom(address, address.BitLen())}, nil
	}
	if len(t.TrafficCIDRs) > maxTrafficNetworks {
		return nil, fmt.Errorf("%s - at most %d networks are allowed per team", location, maxTrafficNetworks)
	}
	networks := make([]netip.Prefix, 0, len(t.TrafficCIDRs))
	for _, value := range t.TrafficCIDRs {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return nil, fmt.Errorf("%s - parse traffic CIDR: %w", location, err)
		}
		if prefix.Bits() == 0 || prefix.Addr().Is4In6() || prefix != prefix.Masked() {
			return nil, fmt.Errorf("%s - traffic CIDR must be canonical and narrower than /0", location)
		}
		networks = append(networks, prefix)
	}
	return networks, nil
}
