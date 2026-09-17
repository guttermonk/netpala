package config

import (
	"errors"
	"netpala/common"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/charmbracelet/bubbles/key"
)

// KeyBinding represents a single configurable key binding
type KeyBinding struct {
	Keys []string `toml:"keys"`
	Help string   `toml:"help"`
}

// KeyBindings holds all configurable keybindings for the application
type KeyBindings struct {
	// Navigation
	Up       KeyBinding `toml:"up"`
	Down     KeyBinding `toml:"down"`
	NextPane KeyBinding `toml:"next_pane"`
	PrevPane KeyBinding `toml:"prev_pane"`

	// Actions
	Select            KeyBinding `toml:"select"`
	Remove            KeyBinding `toml:"remove"`
	Scan              KeyBinding `toml:"scan"`
	ToggleAutoConnect KeyBinding `toml:"toggle_autoconnect"`
	ToggleHidden      KeyBinding `toml:"toggle_hidden"`
	SetDns            KeyBinding `toml:"set_dns"`
	SetMac            KeyBinding `toml:"set_mac"`
	ImportVpn         KeyBinding `toml:"import_vpn"`

	// Application
	Quit   KeyBinding `toml:"quit"`
	Cancel KeyBinding `toml:"cancel"`
}

// Colors holds all color configurations for the application
type Colors struct {
	// Primary colors
	Primary     string `toml:"primary"`      // Default text and UI elements
	Active      string `toml:"active"`       // Active/selected borders
	ActiveText  string `toml:"active_text"`  // Active/selected text
	SelectionBg string `toml:"selection_bg"` // Selection bar background
	Inactive    string `toml:"inactive"`     // Inactive/dimmed elements
	Error       string `toml:"error"`        // Error states
	ErrorText   string `toml:"error_text"`   // Error text
	HelpText    string `toml:"help_text"`    // Help text at bottom of window
}

// DNS holds settings for the DNS provider switcher
type DNS struct {
	// DnscryptAddresses is where a local DNSCrypt proxy listens. It must be on
	// port 53, since resolv.conf has no way to express a port.
	DnscryptAddresses []string `toml:"dnscrypt_addresses"`
}

// Security lists systemd units shown in the Security pane. Units that are not
// installed are skipped at runtime, so listing one costs nothing.
type Security struct {
	Services []common.SecurityServiceConfig `toml:"services"`
}

// VPN lists vendor VPN daemons shown in the VPN pane beside NetworkManager
// profiles. Providers whose unit is not installed are skipped at runtime.
type VPN struct {
	Providers []common.VpnProviderConfig `toml:"providers"`
}

// Config holds the entire application configuration
type Config struct {
	KeyBindings KeyBindings `toml:"keybindings"`
	Colors      Colors      `toml:"colors"`
	DNS         DNS         `toml:"dns"`
	Security    Security    `toml:"security"`
	VPN         VPN         `toml:"vpn"`
}

// DefaultVPN returns the vendor VPN daemons netpala knows about out of the box.
//
// Only Mullvad. The "connected" marker for a daemon row comes from whether its
// tunnel interface is up, which is exact for a provider that creates the
// device on connect and removes it on disconnect -- Mullvad does. A provider
// that keeps its interface around whenever the daemon runs would show as
// connected while logically disconnected, and shipping a default that lies is
// worse than shipping none.
func DefaultVPN() VPN {
	return VPN{
		Providers: []common.VpnProviderConfig{
			{
				Name:       "Mullvad",
				Unit:       "mullvad-daemon.service",
				Interface:  "wg0-mullvad",
				Connect:    []string{"mullvad", "connect"},
				Disconnect: []string{"mullvad", "disconnect"},
			},
		},
	}
}

// DefaultSecurity returns the default Security pane configuration
func DefaultSecurity() Security {
	return Security{
		Services: []common.SecurityServiceConfig{
			// StateFile records the last on/off choice so a boot-time unit can
			// replay it. The paths match what the contrib NixOS module derives
			// from the unit name; if the two disagree the choice is written
			// and then never read, and the service quietly reverts on reboot.
			// The confirm texts describe the contrib module's ruleset and a
			// stock i2pd. Both are start-only, and both lead with what leaves
			// the machine, since that is the part a user cannot see for
			// themselves once it is running.
			{
				Name:      "Tor",
				Unit:      "tor-transparent.service",
				StateFile: "/var/lib/netpala/tor-transparent",
				Confirm: "Route all system traffic through Tor?\n\n" +
					"Shared: your ISP sees that you are using Tor, and every " +
					"site you reach sees a Tor exit address rather than yours.\n\n" +
					"Dropped: all UDP except DNS, all ICMP, and IPv6 is " +
					"rejected outright. QUIC, VPNs, VoIP, games, NTP clock " +
					"sync and local network discovery stop working until this " +
					"is switched back off.",
			},
			// Listed under both names because nixpkgs renamed the option and
			// the unit with it. Whichever exists resolves; if both do, one is
			// an alias of the other and the duplicate is dropped.
			{
				Name:        "DNSCrypt",
				Unit:        "dnscrypt-proxy.service",
				StateFile:   "/var/lib/netpala/dnscrypt-proxy",
				ProvidesDNS: true,
			},
			{
				Name:        "DNSCrypt",
				Unit:        "dnscrypt-proxy2.service",
				StateFile:   "/var/lib/netpala/dnscrypt-proxy2",
				ProvidesDNS: true,
			},
			{
				Name:      "I2P",
				Unit:      "i2pd.service",
				StateFile: "/var/lib/netpala/i2pd",
				Confirm: "Make this machine an I2P router?\n\n" +
					"Shared: your IP address, which every I2P peer you " +
					"connect to can see, and bandwidth and CPU spent " +
					"relaying other users' traffic. Relaying is on by " +
					"default - in I2P every router carries traffic, unlike " +
					"Tor where that is a separate role.\n\n" +
					"Not shared: any of your own data. What you relay is " +
					"encrypted, you cannot read it, and it never leaves I2P " +
					"for the clearnet. This is not an exit node.",
			},
		},
	}
}

// DefaultDNS returns the default DNS switcher configuration
func DefaultDNS() DNS {
	return DNS{DnscryptAddresses: common.DefaultDNSCryptAddresses}
}

// DefaultKeyBindings returns the default keybinding configuration
func DefaultKeyBindings() KeyBindings {
	return KeyBindings{
		Up: KeyBinding{
			Keys: []string{"k", "up"},
			Help: "Up",
		},
		Down: KeyBinding{
			Keys: []string{"j", "down"},
			Help: "Down",
		},
		NextPane: KeyBinding{
			Keys: []string{"tab"},
			Help: "Next",
		},
		PrevPane: KeyBinding{
			Keys: []string{"shift+tab"},
			Help: "Prev",
		},
		Select: KeyBinding{
			Keys: []string{"enter", " "},
			Help: "Dis/Connect",
		},
		Remove: KeyBinding{
			Keys: []string{"backspace", "delete"},
			Help: "Remove",
		},
		Scan: KeyBinding{
			Keys: []string{"s"},
			Help: "Scan",
		},
		ToggleAutoConnect: KeyBinding{
			Keys: []string{"a"},
			Help: "Auto",
		},
		ToggleHidden: KeyBinding{
			Keys: []string{"h"},
			Help: "Hidden",
		},
		SetDns: KeyBinding{
			Keys: []string{"d"},
			Help: "DNS",
		},
		SetMac: KeyBinding{
			Keys: []string{"m"},
			Help: "MAC",
		},
		ImportVpn: KeyBinding{
			Keys: []string{"i"},
			Help: "Import",
		},
		Quit: KeyBinding{
			Keys: []string{"q", "ctrl+c", "ctrl+q", "ctrl+w"},
			Help: "Quit",
		},
		Cancel: KeyBinding{
			Keys: []string{"esc"},
			Help: "Cancel",
		},
	}
}

// DefaultColors returns the default color configuration
func DefaultColors() Colors {
	return Colors{
		Primary:     "#a7abca", // Light blue-gray
		Active:      "#9cca69", // Green
		ActiveText:  "#cda162", // Orange
		SelectionBg: "#5a6988", // Darker blue-gray for better contrast
		Inactive:    "#444a66", // Dark gray
		Error:       "#ff0000", // Red
		ErrorText:   "#aa0000", // Dark red
		HelpText:    "#a7abca", // Help text at bottom (same as Primary by default)
	}
}

// DefaultConfig returns a new Config with default values
func DefaultConfig() Config {
	return Config{
		KeyBindings: DefaultKeyBindings(),
		Colors:      DefaultColors(),
		DNS:         DefaultDNS(),
		Security:    DefaultSecurity(),
		VPN:         DefaultVPN(),
	}
}

// GetConfigPath returns the path to the config file following XDG specification
func GetConfigPath() (string, error) {
	configDir := os.Getenv("XDG_CONFIG_HOME")
	if configDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		configDir = filepath.Join(homeDir, ".config")
	}

	return filepath.Join(configDir, "netpala", "config.toml"), nil
}

// Load loads the configuration from the config file
// If the file doesn't exist, it creates one with default values
func Load() (*Config, error) {
	configPath, err := GetConfigPath()
	if err != nil {
		cfg := DefaultConfig()
		return &cfg, nil
	}

	// Check if config file exists
	if _, err := os.Stat(configPath); errors.Is(err, os.ErrNotExist) {
		// Create default config
		cfg := DefaultConfig()
		if saveErr := Save(&cfg); saveErr != nil {
			// If we can't save, just use defaults in memory
			return &cfg, nil
		}
		return &cfg, nil
	}

	// Read existing config
	var cfg Config
	if _, err := toml.DecodeFile(configPath, &cfg); err != nil {
		// If parsing fails, return defaults
		cfg = DefaultConfig()
		return &cfg, nil
	}

	// Merge with defaults to ensure all keys are present
	cfg = mergeWithDefaults(cfg)

	return &cfg, nil
}

// Save saves the configuration to the config file
func Save(cfg *Config) error {
	configPath, err := GetConfigPath()
	if err != nil {
		return err
	}

	// Create directory if it doesn't exist
	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}

	// Create/truncate file
	file, err := os.Create(configPath)
	if err != nil {
		return err
	}
	defer file.Close()

	// Write header comment
	header := `# Netpala Configuration File
# Keybindings can be customized below.
# Available modifiers: ctrl, alt, shift
# Examples: "ctrl+c", "shift+tab", "alt+enter", "a", "up", "down", "left", "right"
# Special keys: enter, space (use " "), tab, backspace, delete, esc, up, down, left, right
# Multiple keys can be assigned to the same action.

`
	if _, err := file.WriteString(header); err != nil {
		return err
	}

	// Encode config
	encoder := toml.NewEncoder(file)
	encoder.Indent = "  "
	return encoder.Encode(cfg)
}

// mergeWithDefaults ensures all keybindings have values, using defaults for missing ones
func mergeWithDefaults(cfg Config) Config {
	defaults := DefaultKeyBindings()
	defaultColors := DefaultColors()

	if len(cfg.KeyBindings.Up.Keys) == 0 {
		cfg.KeyBindings.Up = defaults.Up
	}
	if len(cfg.KeyBindings.Down.Keys) == 0 {
		cfg.KeyBindings.Down = defaults.Down
	}
	if len(cfg.KeyBindings.NextPane.Keys) == 0 {
		cfg.KeyBindings.NextPane = defaults.NextPane
	}
	if len(cfg.KeyBindings.PrevPane.Keys) == 0 {
		cfg.KeyBindings.PrevPane = defaults.PrevPane
	}
	if len(cfg.KeyBindings.Select.Keys) == 0 {
		cfg.KeyBindings.Select = defaults.Select
	}
	if len(cfg.KeyBindings.Remove.Keys) == 0 {
		cfg.KeyBindings.Remove = defaults.Remove
	}
	if len(cfg.KeyBindings.Scan.Keys) == 0 {
		cfg.KeyBindings.Scan = defaults.Scan
	}
	if len(cfg.KeyBindings.ToggleAutoConnect.Keys) == 0 {
		cfg.KeyBindings.ToggleAutoConnect = defaults.ToggleAutoConnect
	}
	if len(cfg.KeyBindings.ToggleHidden.Keys) == 0 {
		cfg.KeyBindings.ToggleHidden = defaults.ToggleHidden
	}
	if len(cfg.KeyBindings.SetDns.Keys) == 0 {
		cfg.KeyBindings.SetDns = defaults.SetDns
	}
	if len(cfg.KeyBindings.SetMac.Keys) == 0 {
		cfg.KeyBindings.SetMac = defaults.SetMac
	}
	if len(cfg.KeyBindings.ImportVpn.Keys) == 0 {
		cfg.KeyBindings.ImportVpn = defaults.ImportVpn
	}
	if len(cfg.KeyBindings.Quit.Keys) == 0 {
		cfg.KeyBindings.Quit = defaults.Quit
	}
	if len(cfg.KeyBindings.Cancel.Keys) == 0 {
		cfg.KeyBindings.Cancel = defaults.Cancel
	}

	// Ensure help text is set
	if cfg.KeyBindings.Up.Help == "" {
		cfg.KeyBindings.Up.Help = defaults.Up.Help
	}
	if cfg.KeyBindings.Down.Help == "" {
		cfg.KeyBindings.Down.Help = defaults.Down.Help
	}
	if cfg.KeyBindings.NextPane.Help == "" {
		cfg.KeyBindings.NextPane.Help = defaults.NextPane.Help
	}
	if cfg.KeyBindings.PrevPane.Help == "" {
		cfg.KeyBindings.PrevPane.Help = defaults.PrevPane.Help
	}
	if cfg.KeyBindings.Select.Help == "" {
		cfg.KeyBindings.Select.Help = defaults.Select.Help
	}
	if cfg.KeyBindings.Remove.Help == "" {
		cfg.KeyBindings.Remove.Help = defaults.Remove.Help
	}
	if cfg.KeyBindings.Scan.Help == "" {
		cfg.KeyBindings.Scan.Help = defaults.Scan.Help
	}
	if cfg.KeyBindings.ToggleAutoConnect.Help == "" {
		cfg.KeyBindings.ToggleAutoConnect.Help = defaults.ToggleAutoConnect.Help
	}
	if cfg.KeyBindings.ToggleHidden.Help == "" {
		cfg.KeyBindings.ToggleHidden.Help = defaults.ToggleHidden.Help
	}
	if cfg.KeyBindings.SetDns.Help == "" {
		cfg.KeyBindings.SetDns.Help = defaults.SetDns.Help
	}
	if cfg.KeyBindings.SetMac.Help == "" {
		cfg.KeyBindings.SetMac.Help = defaults.SetMac.Help
	}
	if cfg.KeyBindings.ImportVpn.Help == "" {
		cfg.KeyBindings.ImportVpn.Help = defaults.ImportVpn.Help
	}
	if cfg.KeyBindings.Quit.Help == "" {
		cfg.KeyBindings.Quit.Help = defaults.Quit.Help
	}
	if cfg.KeyBindings.Cancel.Help == "" {
		cfg.KeyBindings.Cancel.Help = defaults.Cancel.Help
	}

	// Merge colors with defaults
	if cfg.Colors.Primary == "" {
		cfg.Colors.Primary = defaultColors.Primary
	}
	if cfg.Colors.Active == "" {
		cfg.Colors.Active = defaultColors.Active
	}
	if cfg.Colors.ActiveText == "" {
		cfg.Colors.ActiveText = defaultColors.ActiveText
	}
	if cfg.Colors.SelectionBg == "" {
		cfg.Colors.SelectionBg = defaultColors.SelectionBg
	}
	if cfg.Colors.Inactive == "" {
		cfg.Colors.Inactive = defaultColors.Inactive
	}
	if cfg.Colors.Error == "" {
		cfg.Colors.Error = defaultColors.Error
	}
	if cfg.Colors.ErrorText == "" {
		cfg.Colors.ErrorText = defaultColors.ErrorText
	}
	if cfg.Colors.HelpText == "" {
		cfg.Colors.HelpText = defaultColors.HelpText
	}

	if len(cfg.DNS.DnscryptAddresses) == 0 {
		cfg.DNS = DefaultDNS()
	}
	if len(cfg.Security.Services) == 0 {
		cfg.Security = DefaultSecurity()
	}
	if len(cfg.VPN.Providers) == 0 {
		cfg.VPN = DefaultVPN()
	}

	return cfg
}

// ToKeyBinding converts a KeyBinding config to a bubbles key.Binding
func (kb KeyBinding) ToKeyBinding() key.Binding {
	return key.NewBinding(
		key.WithKeys(kb.Keys...),
		key.WithHelp(formatKeysHelp(kb.Keys), kb.Help),
	)
}

// formatKeysHelp creates a help string from keys
func formatKeysHelp(keys []string) string {
	if len(keys) == 0 {
		return ""
	}

	// Map common keys to symbols for compact display
	symbolMap := map[string]string{
		"up":        "↑",
		"down":      "↓",
		"left":      "←",
		"right":     "→",
		"enter":     "⤶",
		" ":         "␣",
		"space":     "␣",
		"tab":       "⇥",
		"shift+tab": "⇤",
		"backspace": "⌫",
		"delete":    "⌦",
		"esc":       "⎋",
		"ctrl+c":    "^C",
		"ctrl+q":    "^Q",
		"ctrl+w":    "^W",
	}

	// Show first two keys max for compact display
	result := ""
	shown := 0
	for _, k := range keys {
		if shown >= 2 {
			break
		}
		if shown > 0 {
			result += "/"
		}
		if sym, ok := symbolMap[k]; ok {
			result += sym
		} else {
			result += k
		}
		shown++
	}

	return result
}

// Matches checks if a key message matches this keybinding
func (kb KeyBinding) Matches(keyStr string) bool {
	for _, k := range kb.Keys {
		if k == keyStr {
			return true
		}
	}
	return false
}

// AppKeyMap holds the application's key bindings in bubbles format
type AppKeyMap struct {
	Up                key.Binding
	Down              key.Binding
	NextPane          key.Binding
	PrevPane          key.Binding
	Select            key.Binding
	Remove            key.Binding
	Scan              key.Binding
	ToggleAutoConnect key.Binding
	ToggleHidden      key.Binding
	SetDns            key.Binding
	SetMac            key.Binding
	ImportVpn         key.Binding
	Quit              key.Binding
	Cancel            key.Binding
}

// NewAppKeyMap creates a new AppKeyMap from a Config
func NewAppKeyMap(cfg *Config) AppKeyMap {
	return AppKeyMap{
		Up:                cfg.KeyBindings.Up.ToKeyBinding(),
		Down:              cfg.KeyBindings.Down.ToKeyBinding(),
		NextPane:          cfg.KeyBindings.NextPane.ToKeyBinding(),
		PrevPane:          cfg.KeyBindings.PrevPane.ToKeyBinding(),
		Select:            cfg.KeyBindings.Select.ToKeyBinding(),
		Remove:            cfg.KeyBindings.Remove.ToKeyBinding(),
		Scan:              cfg.KeyBindings.Scan.ToKeyBinding(),
		ToggleAutoConnect: cfg.KeyBindings.ToggleAutoConnect.ToKeyBinding(),
		ToggleHidden:      cfg.KeyBindings.ToggleHidden.ToKeyBinding(),
		SetDns:            cfg.KeyBindings.SetDns.ToKeyBinding(),
		SetMac:            cfg.KeyBindings.SetMac.ToKeyBinding(),
		ImportVpn:         cfg.KeyBindings.ImportVpn.ToKeyBinding(),
		Quit:              cfg.KeyBindings.Quit.ToKeyBinding(),
		Cancel:            cfg.KeyBindings.Cancel.ToKeyBinding(),
	}
}

// ShortHelp returns key bindings to show in short help
func (k AppKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		k.Up, k.Down, k.Select, k.Remove,
		k.Scan, k.ToggleAutoConnect, k.ToggleHidden, k.SetDns, k.NextPane, k.PrevPane, k.Quit,
	}
}

// PaneHelp returns the bindings that actually do something in the given pane.
//
// Advertising remove, auto-connect, hidden and DNS while the cursor is on the
// VPN list is just noise - those only act on known networks. Trimming the list
// also keeps the bar on one line, which the layout depends on: it budgets
// exactly one row for the status bar, so a wrapped bar pushes the tables off
// the bottom of the screen.
func (k AppKeyMap) PaneHelp(pane int) []key.Binding {
	switch pane {
	case common.PaneKnown:
		return []key.Binding{
			k.Up, k.Down, k.Select, k.Remove, k.Scan,
			k.ToggleAutoConnect, k.ToggleHidden, k.SetDns, k.SetMac,
			k.NextPane, k.PrevPane, k.Quit,
		}
	case common.PaneScanned:
		return []key.Binding{k.Up, k.Down, k.Select, k.Scan, k.NextPane, k.PrevPane, k.Quit}
	case common.PaneVPN:
		// Import is deliberately absent. It is a global key rather than a pane
		// action -- it has to be, since this pane is hidden until there is a
		// profile to put in it, which is exactly the state someone importing
		// their first tunnel is in. Listing it here as well costs a hint the
		// bar does not have room for: the layout budgets one row for it, and
		// a ninth entry pushes pane navigation off the end at 80 columns.
		return []key.Binding{
			k.Up, k.Down, k.Select, k.Remove, k.ToggleAutoConnect, k.SetDns,
			k.NextPane, k.PrevPane, k.Quit,
		}
	case common.PaneSecurity:
		// No remove: a unit is installed by the system, not by netpala, and
		// nothing in this pane is netpala's to delete.
		return []key.Binding{k.Up, k.Down, k.Select, k.NextPane, k.PrevPane, k.Quit}
	case common.PaneDevice:
		return []key.Binding{k.Select, k.Scan, k.NextPane, k.PrevPane, k.Quit}
	}
	return k.ShortHelp()
}

// FullHelp returns the full set of key bindings
func (k AppKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.NextPane, k.PrevPane},
		{k.Select, k.Remove, k.Scan, k.ToggleAutoConnect, k.ToggleHidden, k.SetDns, k.Quit},
	}
}
