package common

import (
	"github.com/godbus/dbus/v5"
)

// // Message sent when device data should be updated.
// type deviceUpdateMsg []Device
// // Message sent when known networks should be updated.
// type knownNetworksUpdateMsg []KnownNetwork
// // Message sent when scanned networks should be updated.
// type scannedNetworksUpdateMsg []ScannedNetwork

// // Message to signal an error from a goroutine.
// type errMsg struct{ err error }

// // Message sent from our periodic timer to trigger a full refresh.
// type periodicRefreshMsg struct{}

// // Message sent from our debounce timer to perform a scan.
// type performScanRefreshMsg struct{}

// Pane identifiers, in the order the panes appear on screen. Stored in
// NetpalaData.selectedBox, so panes that are hidden never hold the selection.
const (
	PaneKnown = iota
	PaneScanned
	PaneVPN
	PaneSecurity
	PaneDevice
)

type VpnUpdateMsg []VpnConnection

// VpnProvidersUpdateMsg carries the vendor-daemon rows. Separate from
// VpnUpdateMsg because the two come from different places and refresh on
// different triggers: profiles follow NetworkManager's signals, providers are
// read from systemd and the kernel on a timer.
type VpnProvidersUpdateMsg []VpnConnection

// RefreshVpnProvidersMsg asks for that re-read. Sent rather than the data
// itself by anything that does not hold the configuration.
type RefreshVpnProvidersMsg struct{}
type SecurityUpdateMsg []SecurityService
type DeviceUpdateMsg []Device
type KnownNetworksUpdateMsg []KnownNetwork
type ScannedNetworksUpdateMsg []ScannedNetwork
type ErrMsg struct{ Err error }

type PeriodicRefreshMsg struct{}
type RefreshKnownNetworksMsg struct{}
type PerformScanRefreshMsg struct{}
type OptimisticAddMsg struct {
	SSID     string
	Security string
}

type ExitFormMsg struct{}
type SubmitEapFormMsg struct {
	Config map[string]string
}
type SubmitConfirmationMsg struct {
	Value bool
}
type SubmitPasswordMsg struct {
	Value string
}

// DnsProbeMsg carries the result of a background resolver liveness check.
type DnsProbeMsg struct {
	Addrs []string
	Err   error
}
type SubmitDnsMsg struct {
	ProviderID string
	// Custom holds the raw address list when ProviderID is "custom".
	Custom string
}
type SubmitMacMsg struct {
	ModeID string
	// Explicit holds the typed address when ModeID is "explicit".
	Explicit string
}

// SubmitVpnImportMsg carries an accepted WireGuard config. The file has
// already been read and parsed by the time this is sent, so the popup can show
// what it contains before anything is written.
type SubmitVpnImportMsg struct {
	Config *WireGuardConfig
	ID     string
	Ifname string
}

// MacRevertedMsg reports that a MAC change was rejected after the profile had
// already been written, and that netpala has put the previous setting back.
//
// Writing the profile always succeeds - it is only a string in a settings map
// - and ActivateConnection returns as soon as the request is queued. A driver
// that will not take a new hardware address fails later, inside the device
// state machine, so without watching for this a rejected MAC looks exactly
// like success while the link stays down.
type MacRevertedMsg struct {
	Iface     string // the device that would not take the address
	Attempted string // the mode that was asked for
	PrevMode  string // the mode restored in its place
	Restored  bool   // whether reconnecting on the old mode worked
}

type Device struct {
	Path         dbus.ObjectPath
	Name         string
	Mode         string
	Powered      bool
	Address      string
	State        int
	CurrentBSSID string
	Scanning     bool
	Frequency    int
	Security     string
}

type KnownNetwork struct {
	Path        dbus.ObjectPath
	BSSID       string
	SSID        string
	Security    string
	Hidden      bool
	AutoConnect bool
	Signal      int
	Connected   bool
	DNSMode     string
	DNSServers  []string
	// MACMode is the profile's MAC behaviour; MACAddress holds the literal
	// address when the mode is explicit.
	MACMode    string
	MACAddress string
}

type ScannedNetwork struct {
	Path     dbus.ObjectPath
	BSSID    string
	SSID     string
	Security string
	Signal   int
}

// SecurityServiceConfig names a systemd unit netpala may show and toggle.
// Toggling also requires a polkit rule permitting manage-units on that unit.
type SecurityServiceConfig struct {
	Name string `toml:"name"`
	Unit string `toml:"unit"`
	// StateFile, when set, records "on"/"off" after a successful toggle so a
	// boot-time unit can replay the choice. NixOS cannot use systemctl enable
	// for this: /etc/systemd/system is a read-only symlink into the store.
	StateFile string `toml:"state_file"`
	// ProvidesDNS marks a unit that answers DNS on loopback. Stopping one
	// while a network profile resolves via loopback takes out name resolution
	// entirely, so netpala asks for confirmation first.
	ProvidesDNS bool `toml:"provides_dns"`
	// Confirm, when set, is shown and must be accepted before this unit is
	// started. Empty means start it straight away.
	//
	// Deliberately free text in config rather than netpala recognising
	// particular unit names: what is worth consenting to depends on how the
	// service is configured on this machine, which netpala cannot infer, and
	// the wording should belong to whoever set it up.
	//
	// Only start is gated. Turning something off needs no consent.
	Confirm string `toml:"confirm"`
}

// SecurityService is the live state of one such unit, read from systemd
// rather than remembered, so external systemctl changes show up correctly.
type SecurityService struct {
	Name        string
	Unit        string
	StateFile   string
	ProvidesDNS bool
	// Confirm is the consent text from config; empty means start without asking.
	Confirm  string
	Active   bool   // ActiveState == "active"
	State    string // ActiveState verbatim: active, inactive, failed, activating
	SubState string
}

// What a VPN row is backed by. The pane lists both, because "route my traffic
// through a provider I have an account with" is one idea to a user however the
// software happens to arrive on the machine.
const (
	// VpnKindProfile is a NetworkManager connection profile. Everything about
	// it is readable and writable over D-Bus.
	VpnKindProfile = iota
	// VpnKindDaemon is a vendor daemon -- Mullvad, Tailscale -- driven by its
	// own CLI. netpala can see whether its tunnel device is up and can run the
	// commands named in config, and that is all: there is no profile to read
	// an endpoint, a DNS setting or an autoconnect flag out of.
	VpnKindDaemon
)

// VpnProviderConfig names a vendor VPN daemon netpala may show and drive.
//
// Deliberately all-config rather than netpala recognising particular vendors:
// the commands, the unit and the interface name differ per provider and per
// packaging, and inventing a built-in list would mean guessing wrong on
// someone's machine with no way for them to correct it.
type VpnProviderConfig struct {
	Name string `toml:"name"`
	// Unit is the daemon's systemd unit. A provider whose unit is not
	// installed is skipped, so listing one costs nothing.
	Unit string `toml:"unit"`
	// Interface is the tunnel device the provider creates. This is how netpala
	// tells "connected" from "the daemon is running", which are not the same
	// thing and differ by the entire point of the feature.
	Interface string `toml:"interface"`
	// Connect and Disconnect are argv, not shell strings. See RunCommand.
	Connect    []string `toml:"connect"`
	Disconnect []string `toml:"disconnect"`
}

type VpnConnection struct {
	Path       dbus.ObjectPath
	ActivePath dbus.ObjectPath
	Name       string
	ConnType   string
	Connected  bool
	// Kind selects which half of this struct is meaningful.
	Kind int
	// Unit, Iface, Connect and Disconnect are set for VpnKindDaemon only.
	Unit       string
	Iface      string
	Connect    []string
	Disconnect []string
	// AutoConnect mirrors connection.autoconnect, which NetworkManager treats
	// as true when the profile does not say otherwise.
	AutoConnect bool
	// Endpoint is the far end of the tunnel: a WireGuard peer's endpoint, or
	// the remote/gateway of a VPN plugin. Which server you are on is the thing
	// the pane most needs to say, and it is not derivable from the name.
	Endpoint string
	// DNSMode and DNSServers are the profile's own resolvers. A tunnel almost
	// always ships its provider's, and while it is up those are what the
	// machine resolves through -- so they belong to the VPN row rather than to
	// the network underneath it.
	DNSMode    string
	DNSServers []string
	// IsDefaultRoute reports that this tunnel is where the machine's traffic
	// actually leaves.
	//
	// Connected is not the same thing. A split-tunnel profile can be up and
	// carrying nothing but its own subnet, which looks identical in the pane
	// and is the opposite of what the user thinks they switched on.
	//
	// Only ever set for VpnKindProfile. NetworkManager answers this for the
	// connections it manages; a vendor daemon's tunnel is not one of them, and
	// the providers that matter here route with fwmark and a policy rule that
	// the main routing table does not show. Netpala cannot tell, so for a
	// daemon row this stays false and nothing is claimed either way -- see
	// KnowsWhatItCarries.
	IsDefaultRoute bool
}

// KnowsWhatItCarries reports whether netpala can tell what this tunnel is
// carrying. False for vendor daemons, whose routing it cannot inspect.
//
// The distinction matters because "not the default route" and "unknown" look
// identical in the struct and mean opposite things to a user deciding whether
// their traffic is protected.
func (v VpnConnection) KnowsWhatItCarries() bool { return v.Kind == VpnKindProfile }

// DNSTarget is a saved connection the DNS picker can act on.
//
// The picker began as a known-networks feature and took a KnownNetwork
// directly. A VPN profile carries the same facts it needs -- a connection
// path, a current setting, and whether it is live -- so both are reduced to
// this rather than the picker learning about two types.
type DNSTarget struct {
	Path dbus.ObjectPath
	// Label is the SSID, or the VPN profile's name.
	Label     string
	Mode      string
	Servers   []string
	Connected bool
	// IsVPN changes how a change is applied. A wireless profile is
	// re-activated on its device; a VPN has to be taken down through its own
	// active connection and brought back up, and the refresh afterwards has to
	// repaint a different pane.
	IsVPN bool
	// ActivePath is the live activation, needed to re-activate a VPN.
	ActivePath dbus.ObjectPath
}

// DNSTargetFromNetwork adapts a wireless profile.
func DNSTargetFromNetwork(n KnownNetwork) DNSTarget {
	return DNSTarget{
		Path:      n.Path,
		Label:     n.SSID,
		Mode:      n.DNSMode,
		Servers:   n.DNSServers,
		Connected: n.Connected,
	}
}

// DNSTargetFromVpn adapts a VPN profile.
func DNSTargetFromVpn(v VpnConnection) DNSTarget {
	return DNSTarget{
		Path:       v.Path,
		Label:      v.Name,
		Mode:       v.DNSMode,
		Servers:    v.DNSServers,
		Connected:  v.Connected,
		IsVPN:      true,
		ActivePath: v.ActivePath,
	}
}

// DnsStateMsg carries what the system is actually resolving through, so the
// UI can compare it against what NetworkManager applied. Read on refresh
// rather than inside key handlers: it touches the filesystem and D-Bus.
type DnsStateMsg struct {
	Effective []string // /etc/resolv.conf
	Applied   []string // what NetworkManager configured
}
