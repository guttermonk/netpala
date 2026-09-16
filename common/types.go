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

type VpnConnection struct {
	Path       dbus.ObjectPath
	ActivePath dbus.ObjectPath
	Name       string
	ConnType   string
	Connected  bool
	// AutoConnect mirrors connection.autoconnect, which NetworkManager treats
	// as true when the profile does not say otherwise.
	AutoConnect bool
	// Endpoint is the far end of the tunnel: a WireGuard peer's endpoint, or
	// the remote/gateway of a VPN plugin. Which server you are on is the thing
	// the pane most needs to say, and it is not derivable from the name.
	Endpoint string
}

// DnsStateMsg carries what the system is actually resolving through, so the
// UI can compare it against what NetworkManager applied. Read on refresh
// rather than inside key handlers: it touches the filesystem and D-Bus.
type DnsStateMsg struct {
	Effective []string // /etc/resolv.conf
	Applied   []string // what NetworkManager configured
}
