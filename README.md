
<div align="center">
  <h2> Netpala (Impala Go Edition) </h2>
</div>


A lightweight (hopefully) terminal-friendly NetworkManager wrapper written in Go.
It’s a clone of Impala, because Impala’s UI made a single majestic white tear roll down my leg.

---

## 📸 Demo

![Netpala](./netpala.png "Netpala")

---

## 💡 Prerequisites

A Linux-based OS with NetworkManager and dbus running.
**Compatible with both wpa_supplicant and iwd backends.**

---

## 🚀 Installation

### Binary Release

Pre-built binaries coming soon.

---

### Install from AUR

> **This fork is not packaged in the AUR.** The `netpala` AUR package tracks
> [joel-sgc/netpala](https://github.com/joel-sgc/netpala), the upstream project
> this was forked from, and does not include the changes here. Install from
> source or with Nix instead.

---

### Install from Source (Go)

```bash
git clone https://github.com/guttermonk/netpala.git
cd netpala
go build
./netpala
```

You'll need:

- Go 1.25.4+
- NetworkManager running
- dbus available

---

### Install with Nix

#### Run directly without installing

```bash
nix run github:guttermonk/netpala
```

#### Install to profile

```bash
nix profile install github:guttermonk/netpala
```

#### Add to NixOS configuration

**1. Add the flake as an input.** In your `flake.nix`:

```nix
{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    netpala.url = "github:guttermonk/netpala";
    # Optional: build netpala against your own nixpkgs rather than its pinned one
    netpala.inputs.nixpkgs.follows = "nixpkgs";
  };

  outputs = { nixpkgs, netpala, ... }@inputs: {
    nixosConfigurations.myhost = nixpkgs.lib.nixosSystem {
      system = "x86_64-linux";
      # Pass inputs through so configuration.nix can reach them
      specialArgs = { inherit inputs; };
      modules = [ ./configuration.nix ];
    };
  };
}
```

**2. Import the module in `configuration.nix`:**

```nix
{ inputs, ... }:
{
  imports = [ inputs.netpala.nixosModules.default ];

  programs.netpala.enable = true;
}
```

That installs netpala system-wide and ensures dbus is enabled. The module also
takes `programs.netpala.package` if you want to override the build.

If you would rather not use the module, add the package directly — you are then
responsible for NetworkManager and dbus yourself:

```nix
{ inputs, pkgs, ... }:
{
  environment.systemPackages = [ inputs.netpala.packages.${pkgs.system}.default ];
}
```

**3. Rebuild:**

```bash
sudo nixos-rebuild switch --flake .#myhost
```

#### Optional: transparent Tor proxying

The Security pane can toggle a transparent Tor proxy, but netpala does not ship
the firewall rules — they live in
[`contrib/tor-transparent/`](contrib/tor-transparent/) so you can review them
before trusting them. Copy that directory next to your `configuration.nix` and:

```nix
{
  imports = [ ./tor-transparent/tor-transparent.nix ];

  services.torTransparent = {
    enable = true;
    # Units netpala may start and stop without a password prompt. Keep in step
    # with the [[security.services]] entries in your netpala config.toml.
    allowedUnits = [
      "tor-transparent.service"
      "dnscrypt-proxy2.service"
    ];
  };
}
```

Enabling the module installs the rules **dormant** — it does not route anything
through Tor until the unit is started, from the Security pane or with
`systemctl start tor-transparent`. See that directory's files for what the
ruleset does and its limitations.

#### Development shell

```bash
git clone https://github.com/guttermonk/netpala.git
cd netpala
nix develop
```

---

### Omarchy / Waybar launcher example

```bash
#!/bin/bash
$TERMINAL --title=com.omarchy.netpala netpala
```

## Hyprland floating window rules
```bash
windowrule = float 1, match:title com.omarchy.netpala
```

---

## 🪄 Usage

### Global

- Tab / Shift+Tab : Switch between sections
- j / Down : Scroll down
- k / Up : Scroll up
- s : Force scan
- q / Ctrl+C: Quit

### Networks

- Space / Enter : Connect / Disconnect
- Delete / Backspace : Remove network
- a : Toggle auto-connect
- h : Toggle hidden
- d : Switch DNS provider

---

### DNS Provider Switcher

Pressing `d` on a known network opens a picker with four choices:

| Option | Servers |
| --- | --- |
| DHCP | none saved — uses whatever the router hands out |
| Cloudflare | `1.1.1.1`, `1.0.0.1` (+ IPv6) |
| Google | `8.8.8.8`, `8.8.4.4` (+ IPv6) |
| DNSCrypt | a local encrypted-DNS proxy, `127.0.0.1` by default |
| Custom | whatever you type, comma or space separated |

The setting is stored on the NetworkManager connection profile, so it is
per-network: your home Wi-Fi can keep the router's DNS while a coffee-shop
network is pinned to Cloudflare. Picking anything other than DHCP also sets
`ignore-auto-dns`, so the DHCP-supplied servers are not appended.

The current provider is shown in the DNS column of the Known Networks table.
If the connection being edited is the active one, netpala re-activates it so
the change applies immediately instead of at the next reconnect.

Picking anything other than DHCP also suppresses the router's IPv6 resolvers,
so a v4-only choice can't leak queries around your chosen provider.

#### DNSCrypt

**netpala does not run a DNSCrypt proxy and cannot speak the protocol.**
DNSCrypt is implemented by a separate daemon such as
[`dnscrypt-proxy`](https://github.com/DNSCrypt/dnscrypt-proxy), which listens on
loopback and encrypts queries upstream. This option just points the connection
profile at that daemon, so you need it installed and running first.

The proxy must listen on **port 53** — `/etc/resolv.conf` has no syntax for a
port, so a proxy on `127.0.0.1:5353` cannot be selected here. Configure the
address in `~/.config/netpala/config.toml`:

```toml
[dns]
dnscrypt_addresses = ["127.0.0.1"]
```

Because pointing at a proxy that isn't running takes out name resolution
entirely, netpala probes the listener when you highlight the row and shows
`(running)` or `(not responding)`. Applying a proxy that isn't answering takes
a second Enter to confirm. Any all-loopback DNS setting reads back as DNSCrypt,
so a non-default listener is still recognised.

---

### Security Pane

An optional pane listing systemd units, toggled with the select key like a VPN.
It is aimed at things that change how traffic leaves the machine — a
transparent Tor proxy, a local DNSCrypt daemon.

```
┌ Security ────────────────────────────────────────────────────────────────────┐
│         Service                       Unit                        State      │
│                                                                              │
│  >        Tor                tor-transparent.service              active     │
│  >      DNSCrypt             dnscrypt-proxy2.service             running     │
└──────────────────────────────────────────────────────────────────────────────┘
```

**The pane hides itself when none of the configured units are installed**, so
it costs nothing if you don't use it. State is read from systemd on every
refresh rather than remembered, so a change made with `systemctl` shows up
correctly — netpala reports what is true, not what it last asked for.

Toggling needs a polkit rule permitting
`org.freedesktop.systemd1.manage-units` for each unit you want to control —
see `allowedUnits` in [`contrib/tor-transparent/`](contrib/tor-transparent/).
Without one you get an error naming the missing rule instead of a silent
failure.

#### DNS and the local resolver are kept in step

`provides_dns = true` marks a unit that answers DNS on loopback. That unit and
the **DNSCrypt** option in the DNS picker are two halves of one thing, so
netpala keeps them consistent for the live connection:

| You do | netpala does |
| --- | --- |
| Switch the resolver **on** here | Points the live connection's DNS at it |
| Pick DNSCrypt for the **active** network | Starts the resolver, then applies the DNS |
| Pick DNSCrypt for an **inactive** network | Saves the setting only — the resolver follows when that network is activated |
| Switch the resolver **off** here | Opens the DNS picker to choose a replacement first |

Ordering matters and is sequenced, never run in parallel: the resolver is
listening *before* `resolv.conf` points at it, and a replacement is applied
*before* the resolver goes away. That leaves no window where name resolution
is pointed at something that isn't there.

Switching the resolver off while the live connection uses it opens the picker
rather than warning you:

```
Switching DNSCrypt off - pick DNS for nikolatesla2 first
```

Cancelling abandons the stop and leaves the resolver up. Choosing DNSCrypt in
that popup reads as "actually, keep it" and also abandons the stop. Saved
networks that aren't connected need no warning at all — their resolver is
started again when you connect to them.

`state_file` records the choice so a boot-time unit can replay it. This is
necessary because NixOS cannot use `systemctl enable` — `/etc/systemd/system`
is a read-only symlink into the Nix store, so there is nowhere to write the
`.wants` symlink.

A worked example — a fail-closed nftables ruleset for transparent Tor plus the
NixOS module, polkit rule and boot-time restore unit — is in
[`contrib/tor-transparent/`](contrib/tor-transparent/).

> **Note:** transparent proxying routes packets; it is not equivalent to Tor
> Browser and the Tor Project does not recommend it as a substitute. It does
> nothing about browser fingerprinting and gives you no stream isolation.

---

## ⚙️ Configuration

Netpala supports customizable keybindings through a configuration file located at `~/.config/netpala/config.toml`.

On first run, a default configuration file will be created automatically. You can also copy the example config:

```bash
mkdir -p ~/.config/netpala
cp config.example.toml ~/.config/netpala/config.toml
```

### Keybinding Configuration

All keybindings can be customized in the config file. Example:

```toml
[keybindings]

# Navigation
[keybindings.up]
keys = ["k", "up"]
help = "Up"

[keybindings.down]
keys = ["j", "down"]
help = "Down"

[keybindings.next_pane]
keys = ["tab"]
help = "Next"

[keybindings.prev_pane]
keys = ["shift+tab"]
help = "Prev"

# Actions
[keybindings.select]
keys = ["enter", " "]
help = "Dis/Connect"

[keybindings.remove]
keys = ["backspace", "delete"]
help = "Remove"

[keybindings.scan]
keys = ["s"]
help = "Scan"

[keybindings.toggle_autoconnect]
keys = ["a"]
help = "Auto"

[keybindings.toggle_hidden]
keys = ["h"]
help = "Hidden"

[keybindings.set_dns]
keys = ["d"]
help = "DNS"

# Application
[keybindings.quit]
keys = ["q", "ctrl+c", "ctrl+q", "ctrl+w"]
help = "Quit"

[keybindings.cancel]
keys = ["esc"]
help = "Cancel"
```

### Available Modifiers and Keys

- **Modifiers**: `ctrl`, `alt`, `shift`
- **Special keys**: `enter`, `space` (use `" "`), `tab`, `backspace`, `delete`, `esc`, `up`, `down`, `left`, `right`
- **Examples**: `"ctrl+c"`, `"shift+tab"`, `"alt+enter"`, `"a"`, `"up"`

Multiple keys can be assigned to the same action by listing them in the `keys` array.

### Color Configuration

You can also customize the application colors in the same config file:

```toml
[colors]
# Default text and UI elements
primary = "#a7abca"        # Light blue-gray

# Active/selected borders
active = "#9cca69"         # Green

# Active/selected text
active_text = "#cda162"    # Orange

# Selection bar background
selection_bg = "#5a6988"   # Darker blue-gray for better contrast

# Inactive/dimmed elements
inactive = "#444a66"       # Dark gray

# Error states
error = "#ff0000"          # Red

# Error text
error_text = "#aa0000"     # Dark red
```

Colors can be specified as:
- Hex color codes: `"#a7abca"`
- Terminal color names: `"red"`, `"blue"`, `"green"`, etc.
- ANSI color numbers: `"1"` (red), `"2"` (green), etc.

---

## 🚀 Features (So Far)

- Lists available network devices
- Shows known and scanned networks
- Shows VPN connections
- Add & connect to:
  - WPA-PSK
  - WPA-SAE
  - WPA-Enterprise (EAP)
  - Open Networks
- Force network scan
- Enable/Disable devices
- Toggle auto-connect and hidden per known network
- Per-network DNS provider switcher (DHCP / Cloudflare / Google / DNSCrypt / Custom)
- Optional Security pane for toggling systemd units (transparent Tor, DNSCrypt)
- Communicates with NetworkManager + wpa_supplicant over DBus

---

## ⚠️ Missing / TODO

- VPN manager UI Testing (Should work but haven't been able to test 100%)
- Probably some bugs (Hopefully there's nothing)

---

## 🧩 Implementation Notes

The DBus code was mostly vibe-coded.
It works. I don’t care. If you do care, fork it and figure it out.

---

## 🧾 License

Do What the Fuck You Want To Public License (WTFPL)

---

## ❤️ Closing Thoughts

I built this for myself because I wanted something that just works, and this just works.
If you like it, awesome. If not, feel free to improve it or ignore it entirely.