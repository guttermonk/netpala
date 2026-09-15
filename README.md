
<div align="center">
  <h2> Netpala (Impala Go Edition) </h2>
</div>


A lightweight (hopefully) terminal-friendly NetworkManager wrapper written in Go.
It’s a clone of Impala, whose interface was good enough to be worth having in Go.

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
      "i2pd.service"
    ];
  };
}
```

Enabling the module installs the rules **dormant** — it does not route anything
through Tor until the unit is started, from the Security pane or with
`systemctl start tor-transparent`. See that directory's files for what the
ruleset does and its limitations.

#### Optional: I2P

I2P needs nothing netpala-specific — the Security pane is a generic systemd
toggle and `i2pd.service` is in the default service list:

```nix
services.i2pd = {
  enable = true;
  # How applications reach I2P. Off by default, so without it the daemon
  # runs and nothing can use it.
  proto.sam.enable = true;
  # Web console on http://127.0.0.1:7070. Joining I2P takes minutes, and this
  # is the only way to see whether tunnels are built rather than inferring it
  # from an application that is silently getting nowhere.
  proto.http.enable = true;
};
services.torTransparent.directUsers = [ "i2pd" ];
```

**Tor, DNSCrypt and I2P can all run at once**, which is what that second line
is for. They are not alternatives: each owns a different class of traffic —
DNSCrypt answers DNS, Tor carries clearnet and `.onion`, and I2P carries
`.i2p`, an address space Tor cannot reach at all.

For that to work, i2pd's own traffic has to reach I2P peers **directly**.
Without the exemption the ruleset drops it: I2P's main transport is UDP, which
is dropped as unroutable through Tor, and its TCP would be redirected into
Tor's TransPort, where exits refuse the ports I2P peers use. Tunnelling I2P
through Tor is not the alternative — it breaks I2P's transport and buys
nothing, since I2P is itself an anonymity network.

`directUsers` is empty by default because it is a deliberate hole in a
fail-closed ruleset, and enabling the module should not quietly open one. With
i2pd listed, its traffic leaves directly: still I2P-encrypted and only to I2P
peers, so nothing is in plaintext, but **identifiable as I2P on the wire**. If
your threat model includes hiding that you use an anonymity network at all,
that matters; if it is anonymising what you do, I2P provides that itself.

Note that I2P is an overlay for I2P-internal services rather than a
general-purpose exit, so there is no transparent-proxy equivalent to the Tor
setup. Applications opt in: a browser through the HTTP proxy on
`127.0.0.1:4444` or SOCKS on `4447`, and anything needing its own tunnels —
a BitTorrent client, say — through SAM on `7656`.

Two things to expect. **Addresses have to be inside I2P**: a `.i2p` site, or a
torrent whose swarm and tracker are on the network. A clearnet address will not
resolve over I2P however the client is configured, because there is nothing
there to reach. And it is **slow** — a high-latency overlay by design, fine for
long-running transfers and frustrating if you expect clearnet speeds.

If you have listed `i2pd.service` in `managedUnits`, remember it no longer
starts at boot: turn it on in the Security pane and give it a few minutes to
build tunnels before expecting anything to work.

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
- m : Switch MAC address mode

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

### MAC Address Switcher

Pressing `m` on a known network chooses what hardware address the interface
presents when it joins:

| Option | Behaviour |
| --- | --- |
| Default | whatever NetworkManager's global setting says |
| Stable | derived from the network — same every time you rejoin, different per network |
| Random | a fresh address on every connection |
| Permanent | the real hardware address |
| Explicit | an address you type |

**Stable is usually the right choice.** Your MAC is a permanent hardware
identifier broadcast to every access point you associate with, and no amount of
DNS encryption or onion routing hides it — a network you have joined before
recognises you, and networks can be correlated with each other. Stable breaks
that correlation while keeping the address consistent per network, so captive
portals and MAC allowlists carry on working. Random is stronger but re-triggers
portal logins; permanent is what you want only where a network identifies you
by address deliberately.

The mode is shown in the MAC column of the Known Networks table. Changing it
re-activates the connection, because the address is chosen when the interface
associates.

Explicit addresses are validated: a multicast address (odd first octet) is
rejected, since an interface claiming one would not receive its own traffic.

> **Your adapter has to support it.** Some drivers - Broadcom `wl` among
> them - advertise no MAC randomisation, and NetworkManager's attempts to set
> the address fail in the driver. Check with `iw list | grep -i randomis`
> before relying on Stable or Random; where it is unsupported, Permanent is
> the only mode that behaves.

> **Scanning is separate.** This setting covers association only. Wi-Fi
> scanning broadcasts an address too, controlled globally rather than per
> network — on NixOS, `networking.networkmanager.wifi.scanRandMacAddress = true`.

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
it costs nothing if you don't use it. I2P is listed by default too:
`i2pd` needs no netpala-specific support - the pane is a generic systemd
toggle, so any unit works. Note that I2P is an overlay network for I2P-internal
services rather than a general-purpose exit, so there is no transparent-proxy
equivalent to the Tor setup. State is read from systemd on every
refresh rather than remembered, so a change made with `systemctl` shows up
correctly — netpala reports what is true, not what it last asked for.

Toggling needs a polkit rule permitting
`org.freedesktop.systemd1.manage-units` for each unit you want to control —
see `allowedUnits` in [`contrib/tor-transparent/`](contrib/tor-transparent/).
Without one you get an error naming the missing rule instead of a silent
failure.

#### Asking before starting a service

`confirm = "..."` puts the text in front of the user and waits for an answer
before the unit is started. Empty means start it straight away. **Only starting
is gated** — turning something off never asks, since a prompt between you and
switching a thing back off is exactly the wrong place for one.

It is free text in config rather than netpala recognising particular unit
names: what is worth consenting to depends on how the service is configured on
*your* machine, which netpala cannot infer.

Tor and I2P ship with one by default, because both change what leaves the
machine in ways you cannot see from the pane afterwards:

```toml
[[security.services]]
name    = "I2P"
unit    = "i2pd.service"
confirm = """
Make this machine an I2P router?

Shared: your IP address, which every I2P peer you connect to can see, and \
bandwidth and CPU spent relaying other users' traffic.

Not shared: any of your own data. What you relay is encrypted, you cannot \
read it, and it never leaves I2P for the clearnet. This is not an exit node.
"""
```

The default I2P text is worth reading once even if you never change it. I2P is
not Tor: every router relays, so enabling `i2pd` makes you a participant rather
than a client — `notransit = false` and `transittunnels = 2500` are stock
defaults. It is *not* an exit node, though; transit traffic never reaches the
clearnet, so the liability reasoning people carry over from Tor exits does not
apply.

The Tor text names what stops working, which is the part that bites: the
ruleset is fail-closed, so all UDP except DNS, all ICMP and IPv6 are dropped.
QUIC, VPNs, VoIP, games, NTP clock sync and local network discovery go with
them until it is switched off.

DNSCrypt deliberately ships without one — it is low-stakes, and the DNS picker
already guards the case that matters.

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

#### Keeping a service off across reboots

By default the daemons run continuously and netpala toggles only the routing.
If you would rather netpala own whether they run at all — off at home for the
speed, on when travelling — list them as `managedUnits`:

```nix
services.torTransparent.managedUnits = [
  { unit = "tor-transparent.service"; }
  { unit = "dnscrypt-proxy.service"; }
  { unit = "i2pd.service"; }
];
```

Each listed unit is taken out of `multi-user.target`, so it no longer starts
merely because it is enabled, and is started at boot only when its state file
says it was on. Without that, stopping a service lasts until the next reboot
and no longer. `allowedUnits` is derived from this list, so the polkit rule
does not need to repeat it.

**Use canonical unit names.** A renamed unit keeps its old name as an alias,
and the two are not interchangeable here: overriding the alias defines a
second, unrelated unit while the real one carries on starting at boot. netpala
resolves aliases to the canonical name via systemd's `Id`, so its own config
may use either, but Nix cannot.

> **If you manage the DNS daemon, clear `networking.nameservers`.** The
> dnscrypt module points it at `127.0.0.1`, which outranks NetworkManager in
> resolvconf. With the daemon toggled off, resolv.conf would still name a dead
> resolver and there would be no DNS at boot:
> ```nix
> networking.nameservers = lib.mkForce [ ];
> ```
> That hands DNS back to NetworkManager, so netpala's per-network setting
> decides — and turning the resolver off moves the profile with it.

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