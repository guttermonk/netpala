package common

import (
	"fmt"
	"net"
	"strings"
)

// MAC address modes for a wireless profile.
//
// These are NetworkManager's own keywords for 802-11-wireless.cloned-mac-address.
// Over D-Bus they live in the "assigned-mac-address" string property; the
// "cloned-mac-address" byte array is a legacy mirror that only ever holds an
// explicit address.
const (
	MACModeDefault   = "" // unset: NetworkManager's global default applies
	MACModePermanent = "permanent"
	MACModePreserve  = "preserve"
	MACModeRandom    = "random"
	MACModeStable    = "stable"
	MACModeExplicit  = "explicit" // a literal address, held in MACAddress
)

// MACOption is a selectable MAC behaviour.
type MACOption struct {
	ID    string
	Label string // short form, for the known-networks table
	Desc  string // long form, for the picker
}

// MACOptions is the list offered by the switcher, in display order.
//
// Stable is first among the privacy options because it is the one that keeps
// captive portals and MAC allowlists working: the address is derived from the
// network, so it is the same every time you rejoin, but different per network
// so you cannot be followed between them.
var MACOptions = []MACOption{
	{
		ID:    MACModeDefault,
		Label: "Default",
		Desc:  "NetworkManager's setting",
	},
	{
		ID:    MACModeStable,
		Label: "Stable",
		Desc:  "same per network, unlinkable across them",
	},
	{
		ID:    MACModeRandom,
		Label: "Random",
		Desc:  "new address every connection",
	},
	{
		ID:    MACModePermanent,
		Label: "Permanent",
		Desc:  "the real hardware address",
	},
	{
		ID:    MACModeExplicit,
		Label: "Explicit",
		Desc:  "an address you choose",
	},
}

// MACOptionByID looks up an option, falling back to the default.
func MACOptionByID(id string) MACOption {
	for _, o := range MACOptions {
		if o.ID == id {
			return o
		}
	}
	return MACOptions[0]
}

// MACModeLabel is the short name shown in the known-networks table.
func MACModeLabel(id string) string {
	return MACOptionByID(id).Label
}

// MatchMACMode maps the stored assigned-mac-address value onto a mode. An
// address that is not one of the keywords is a literal MAC.
func MatchMACMode(assigned string) string {
	switch strings.ToLower(strings.TrimSpace(assigned)) {
	case "":
		return MACModeDefault
	case MACModePermanent:
		return MACModePermanent
	case MACModePreserve:
		return MACModePreserve
	case MACModeRandom:
		return MACModeRandom
	case MACModeStable:
		return MACModeStable
	}
	return MACModeExplicit
}

// ParseMAC validates a user-supplied hardware address.
//
// Multicast addresses are rejected: the low bit of the first octet marks a
// frame as multicast, so an interface claiming one would not receive its own
// unicast traffic.
func ParseMAC(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("no address given")
	}
	hw, err := net.ParseMAC(s)
	if err != nil {
		return "", fmt.Errorf("%q is not a valid MAC address", s)
	}
	if len(hw) != 6 {
		return "", fmt.Errorf("%q is not a 6-byte Ethernet address", s)
	}
	if hw[0]&0x01 != 0 {
		return "", fmt.Errorf("%s is a multicast address; the first octet must be even", hw)
	}
	return hw.String(), nil
}
