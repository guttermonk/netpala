package dbus

import (
	"fmt"

	"netpala/common"
	"netpala/network"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/godbus/dbus/v5"
	"github.com/google/uuid"
)

// Importing a wg-quick configuration as a NetworkManager profile.
//
// The translation is not one-to-one. wg-quick keeps everything in one file,
// while NetworkManager splits the same information across three settings
// groups: the keys and peers are "wireguard", the tunnel's own address and
// resolvers are ordinary "ipv4"/"ipv6", and the interface name belongs to
// "connection" because NM creates the link itself.

// WireGuardSettings builds the NetworkManager settings for an imported config.
//
// Separate from the D-Bus call so the mapping can be tested for real. Getting
// it wrong produces a profile NetworkManager accepts and then cannot bring up,
// which is a bad thing to only find out about against a live bus.
func WireGuardSettings(cfg *common.WireGuardConfig, id, ifname, connUUID string) (map[string]map[string]dbus.Variant, error) {
	if cfg == nil {
		return nil, fmt.Errorf("no configuration to import")
	}

	peers, err := wireguardPeers(cfg.Peers)
	if err != nil {
		return nil, err
	}

	wg := map[string]dbus.Variant{
		"private-key": dbus.MakeVariant(cfg.PrivateKey),
		// Flag 0 is "stored in the system connection". The alternative is an
		// agent-owned secret, which would need netpala running to bring the
		// tunnel up -- wrong for something that should survive a reboot.
		"private-key-flags": dbus.MakeVariant(uint32(0)),
		"peers":             dbus.MakeVariant(peers),
	}
	if cfg.ListenPort != 0 {
		wg["listen-port"] = dbus.MakeVariant(cfg.ListenPort)
	}
	if cfg.MTU != 0 {
		wg["mtu"] = dbus.MakeVariant(cfg.MTU)
	}
	if cfg.FwMark != 0 {
		// NetworkManager picks its own mark when this is 0 and it needs one for
		// the default-route policy rule; an explicit mark in the file is the
		// author's choice and NM will use it for the same purpose.
		wg["fwmark"] = dbus.MakeVariant(cfg.FwMark)
	}

	settings := map[string]map[string]dbus.Variant{
		"connection": {
			"id":   dbus.MakeVariant(id),
			"uuid": dbus.MakeVariant(connUUID),
			"type": dbus.MakeVariant("wireguard"),
			// Required for wireguard: NM creates this link rather than
			// attaching to an existing device.
			"interface-name": dbus.MakeVariant(ifname),
			// Off on import. A full-tunnel config moves every packet the
			// machine sends to a provider, and doing that from the next boot
			// onwards is not something importing a file should decide.
			"autoconnect": dbus.MakeVariant(false),
		},
		"wireguard": wg,
	}

	if err := applyTunnelAddresses(settings, cfg); err != nil {
		return nil, err
	}
	return settings, nil
}

// wireguardPeers encodes the [Peer] sections as NM's "aa{sv}".
func wireguardPeers(peers []common.WireGuardPeer) ([]map[string]dbus.Variant, error) {
	if len(peers) == 0 {
		return nil, fmt.Errorf("the config has no peers")
	}

	out := make([]map[string]dbus.Variant, 0, len(peers))
	for i, p := range peers {
		if p.PublicKey == "" {
			return nil, fmt.Errorf("peer %d has no public key", i+1)
		}

		peer := map[string]dbus.Variant{
			"public-key":  dbus.MakeVariant(p.PublicKey),
			"allowed-ips": dbus.MakeVariant(p.AllowedIPs),
		}
		if p.Endpoint != "" {
			peer["endpoint"] = dbus.MakeVariant(p.Endpoint)
		}
		if p.PresharedKey != "" {
			peer["preshared-key"] = dbus.MakeVariant(p.PresharedKey)
			peer["preshared-key-flags"] = dbus.MakeVariant(uint32(0))
		}
		if p.PersistentKeepalive != 0 {
			peer["persistent-keepalive"] = dbus.MakeVariant(p.PersistentKeepalive)
		}
		out = append(out, peer)
	}
	return out, nil
}

// applyTunnelAddresses fills in the ipv4 and ipv6 sections.
//
// The tunnel's own address comes from [Interface] Address, which is an ordinary
// static address as far as NetworkManager is concerned -- there is no DHCP
// inside a WireGuard tunnel, so the method is always "manual" for a family the
// config gives an address for, and "disabled" for one it does not.
func applyTunnelAddresses(settings map[string]map[string]dbus.Variant, cfg *common.WireGuardConfig) error {
	v4 := map[string]dbus.Variant{}
	v6 := map[string]dbus.Variant{}

	var v4Addrs, v6Addrs []map[string]dbus.Variant
	for _, a := range cfg.Addresses {
		ip, prefix, isV6, err := common.ParseWireGuardAddress(a)
		if err != nil {
			return err
		}
		entry := map[string]dbus.Variant{
			"address": dbus.MakeVariant(ip),
			"prefix":  dbus.MakeVariant(prefix),
		}
		if isV6 {
			v6Addrs = append(v6Addrs, entry)
		} else {
			v4Addrs = append(v4Addrs, entry)
		}
	}

	if len(v4Addrs) == 0 && len(v6Addrs) == 0 {
		return fmt.Errorf("the config gives the tunnel no address")
	}

	// Split the resolvers the same way, since each family's nameservers live in
	// its own section and are encoded differently.
	var dnsV4, dnsV6 []string
	for _, d := range cfg.DNS {
		if _, _, isV6, err := common.ParseWireGuardAddress(d); err == nil && isV6 {
			dnsV6 = append(dnsV6, d)
		} else {
			dnsV4 = append(dnsV4, d)
		}
	}

	if len(v4Addrs) > 0 {
		v4["method"] = dbus.MakeVariant("manual")
		v4["address-data"] = dbus.MakeVariant(v4Addrs)
		if len(dnsV4) > 0 {
			variant, err := network.DNSv4Variant(dnsV4)
			if err != nil {
				return err
			}
			v4["dns"] = variant
			// Nothing hands out resolvers inside the tunnel, so there are no
			// automatic ones to merge with -- but saying so explicitly keeps
			// the ones from the underlying network out of the way.
			v4["ignore-auto-dns"] = dbus.MakeVariant(true)
		}
	} else {
		v4["method"] = dbus.MakeVariant("disabled")
	}

	if len(v6Addrs) > 0 {
		v6["method"] = dbus.MakeVariant("manual")
		v6["address-data"] = dbus.MakeVariant(v6Addrs)
		if len(dnsV6) > 0 {
			variant, err := network.DNSv6Variant(dnsV6)
			if err != nil {
				return err
			}
			v6["dns"] = variant
			v6["ignore-auto-dns"] = dbus.MakeVariant(true)
		}
	} else {
		v6["method"] = dbus.MakeVariant("disabled")
	}

	if len(cfg.SearchDomains) > 0 {
		// Attached to whichever families are actually configured; NM rejects
		// properties set on a disabled section.
		if len(v4Addrs) > 0 {
			v4["dns-search"] = dbus.MakeVariant(cfg.SearchDomains)
		}
		if len(v6Addrs) > 0 {
			v6["dns-search"] = dbus.MakeVariant(cfg.SearchDomains)
		}
	}

	settings["ipv4"] = v4
	settings["ipv6"] = v6
	return nil
}

// AddWireGuardCmd saves an imported config as a NetworkManager profile.
//
// It does not activate it. Importing a file and routing every packet through a
// provider are separate decisions, and the pane is one keypress away once the
// profile exists.
func AddWireGuardCmd(conn *dbus.Conn, cfg *common.WireGuardConfig, id, ifname string) tea.Cmd {
	return func() tea.Msg {
		newUUID, err := uuid.NewRandom()
		if err != nil {
			return common.ErrMsg{Err: fmt.Errorf("failed to generate uuid: %w", err)}
		}

		settings, err := WireGuardSettings(cfg, id, ifname, newUUID.String())
		if err != nil {
			return common.ErrMsg{Err: err}
		}

		settingsObj := conn.Object(network.NMDest, "/org/freedesktop/NetworkManager/Settings")
		call := settingsObj.Call("org.freedesktop.NetworkManager.Settings.AddConnection", 0, settings)
		if call.Err != nil {
			return common.ErrMsg{Err: fmt.Errorf("failed to add WireGuard connection %q: %w", id, call.Err)}
		}

		// The Settings.NewConnection signal drives the pane refresh, but the
		// listener is only re-armed by some message paths; refreshing here too
		// means the new row appears whether or not the signal is caught.
		return common.VpnUpdateMsg(network.GetVpnData(conn))
	}
}
