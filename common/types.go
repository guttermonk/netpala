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
}

// SecurityService is the live state of one such unit, read from systemd
// rather than remembered, so external systemctl changes show up correctly.
type SecurityService struct {
	Name        string
	Unit        string
	StateFile   string
	ProvidesDNS bool
	Active      bool   // ActiveState == "active"
	State       string // ActiveState verbatim: active, inactive, failed, activating
	SubState    string
}

type VpnConnection struct {
	Path       dbus.ObjectPath
	ActivePath dbus.ObjectPath
	Name       string
	ConnType   string
	Connected  bool
}

// DnsStateMsg carries what the system is actually resolving through, so the
// UI can compare it against what NetworkManager applied. Read on refresh
// rather than inside key handlers: it touches the filesystem and D-Bus.
type DnsStateMsg struct {
	Effective []string // /etc/resolv.conf
	Applied   []string // what NetworkManager configured
}
