# VPN Manager — plan

Working notes for the `vpn-manager` branch, forked from `security-options` at
`936b52f`. This is a design document, not a promise: the phases are ordered so
each one is useful on its own and can be shipped without the ones after it.

---

## Where this starts from

Netpala already has a VPN pane. `network.GetVpnData` lists saved NetworkManager
profiles whose `connection.type` is `vpn` or `wireguard`, and the select key
hands the chosen row to `dbus.ToggleVpnCmd`. That is the whole feature: list and
toggle.

So this is not a new section. It is turning a read-and-toggle pane into
something that can add, remove and describe what it lists.

---

## Which pane a thing belongs in

The panes are organised by **what the user is doing**, not by how netpala talks
to the thing.

> **VPN pane** — route my traffic through **one operator I have a relationship
> with**: an account, usually a payment. One hop, full speed. That operator can
> correlate me with everything I do.
>
> **Security pane** — change this machine's **security or anonymity posture**,
> where **no single operator sees both ends**: Tor, I2P, DNSCrypt.

An earlier draft of this plan drew the line at "NetworkManager profile vs
systemd unit". That was wrong — it organised the UI around netpala's
implementation rather than around the user's intent, and it put Mullvad in the
Security pane next to Tor, which nobody would go looking for.

The rule above puts commercial VPNs in the VPN pane whether they arrive as an
NM profile or as a vendor daemon, and keeps Tor in Security for a reason worth
stating to a user rather than an incidental one.

### Why Tor is not a VPN

Worth writing down, since it is the question the pane boundary turns on.

| | VPN | Tor |
|---|---|---|
| Path | One hop to one server you chose | Three relays, each knowing only its neighbours |
| Who can correlate you with your traffic | The provider — a single point of trust | No single party; that is the design |
| Speed | Near line rate | Slow |
| On Linux | A network interface, routes, DNS | A local daemon offering SOCKS, plus firewall rules to make it transparent |

A VPN relocates your trust to one company. Tor distributes it so that no single
operator holds both halves. Neither does anything about a logged-in browser
identifying you.

I2P is a third thing again — an overlay network for I2P-internal services rather
than a general-purpose exit, which is why there is no transparent-proxy
equivalent to the Tor setup.

---

## What activating a VPN does to the Wi-Fi connection

It does not drop it, and it must not — the tunnel rides on top of it.

- **`vpn` type** (OpenVPN, OpenConnect): NM layers it as a *secondary*
  connection over the already-active one. That is why `ToggleVpnCmd` passes
  device path `/` — NM picks the primary device itself. Wi-Fi stays
  `activated`, a `tun0` appears, and a default route through it is installed.
- **`wireguard` type** (native, NM ≥ 1.16): NM creates its own `wg0` device.
  Not layered, but it does not touch the Wi-Fi device either.

Either way the Known Networks row keeps its `>` marker, the same as with Tor. If
Wi-Fi drops, the tunnel drops with it.

`GetKnownNetworks` filters to `DeviceType == 2`, so VPN profiles never leak into
that pane.

**This creates a UI honesty problem, tracked as Phase 4.** With a VPN up,
netpala shows `>` on the Wi-Fi *and* `>` on the VPN, with nothing saying that
traffic actually leaves through the tunnel — and the Known Networks pane will
show that profile's DNS setting while the VPN's DNS is the one in use.
`dnsIsOverridden` already grapples with a version of this for the local
resolver. Same problem, new source.

---

## Phase 1 — Make the existing pane a manager — **done**

Small, and reuses commands that already exist.

Landed. Two things turned up that this plan had not accounted for:

- `ToggleAutoConnectCmd` ended by returning `KnownNetworksUpdateMsg`, so
  reusing it for a VPN profile would have written the setting correctly and
  then left the pane the user was looking at showing the old value until the
  15-second tick. Split into `setAutoconnect` plus two thin commands that
  refresh their own pane.
- NetworkManager omits `connection.autoconnect` when it is at its default,
  which is **true**. Reading the absent key as the Go zero value would have
  reported every profile as off.

`DeleteConnectionCmd` needed no VPN-specific work: `ConnectionRemoved` already
fans out to a `VpnUpdateMsg` in `dbus/events.go`.

1. **Delete a profile.** Wire `Remove` on `PaneVPN` in `netpala.go`;
   `dbus.DeleteConnectionCmd` is already generic over connection path. Needs a
   third `confirmAction` constant beside `confirmDeleteNetwork` and
   `confirmStartService`.
2. **Toggle autoconnect.** `dbus.ToggleAutoConnectCmd` is likewise
   path-generic. Add `AutoConnect` to `common.VpnConnection` and read it in
   `GetVpnData`.
3. **An Endpoint column.** `FormatVpnData` shows marker / Name / Type. Add the
   peer `endpoint` for WireGuard and `remote` for OpenVPN, plus an autoconnect
   marker. Which server you are on is the thing a VPN pane most needs to say.
4. **Pane help.** `config.PaneHelp` lumps `PaneVPN` in with `PaneSecurity`.
   Split them so the VPN pane advertises its own keys.

---

## Phase 2 — WireGuard import — **done**

The target: point netpala at a `wg0.conf` from Mullvad, Proton, IVPN or your own
server and get a working profile.

Landed as `common/wgconf.go` (parse), `dbus/vpn.go` (translate and add) and
`models/vpn_import.go` (the popup). Three things this plan had not anticipated:

- **The VPN pane hides itself when empty**, so a key bound to the pane would be
  unreachable for the one user who needs it — someone with no profiles yet.
  Import is a global key instead. It is also *not* in `PaneHelp`: the status bar
  is budgeted at exactly one row, and a ninth hint pushes pane navigation off
  the end at 80 columns. Documented under Global in the README instead.
- **wg-quick directives NM cannot express** (`PostUp`, `Table`, …) are collected
  rather than dropped, and shown in the import summary. A `PostUp` is often a
  killswitch; losing one silently gives you a tunnel that looks right and is
  not.
- **The popup has two stages.** The file is read and summarised — what it
  routes, where the key lands, what was dropped — before anything is written. A
  provider's `.conf` is an opaque download that most people never read.

Import deliberately does not connect, and sets `autoconnect = false`.

### `common/wgconf.go` — parse

The INI-ish `.conf` into a struct. Pure function, no D-Bus, unit-testable —
the same shape as `common/dns.go` and `common/mac.go`, which are both already
tested that way.

### `dbus/vpn.go` — translate and add

The translation is not one-to-one, and that is where the work is:

| `wg0.conf` | NetworkManager |
|---|---|
| `[Interface] PrivateKey` | `wireguard.private-key` |
| `[Interface] Address = 10.x/32` | `ipv4.method = "manual"` + `ipv4.address-data` — **not** a `wireguard` property |
| `[Interface] DNS` | `ipv4.dns` / `ipv6.dns` |
| `[Interface] ListenPort`, `MTU` | `wireguard.listen-port`, `wireguard.mtu` |
| `[Peer] PublicKey` | peer `public-key` |
| `[Peer] Endpoint` | peer `endpoint` |
| `[Peer] AllowedIPs` | peer `allowed-ips` (`as`) |
| `[Peer] PresharedKey` | peer `preshared-key` |
| `[Peer] PersistentKeepalive` | peer `persistent-keepalive` |
| `AllowedIPs = 0.0.0.0/0` | default route, via `peer-routes` / `ip4-auto-default-route` |

`wireguard.peers` is `aa{sv}` — an array of dicts, one per `[Peer]`.
`connection.interface-name` is **required** for a wireguard profile, since NM
creates the link.

Do not hand-roll the DNS encoding: `network.DNSv4Variant` and
`network.DNSv6Variant` already produce the `[]uint32` / `[][]byte` forms NM
wants, and `applyDNSToSettings` documents the `ignore-auto-dns` handling that
goes with them.

Model the command itself on `AddAndConnectEAPCmd`, including the
optimistic-add-then-refresh pattern and the `tea.BatchMsg` return — note the
comment there about `tea.Batch` being silently dropped when returned as a Msg.

### `models/vpn_import.go` — the popup

A file-path field, `PopupState = 5`, following `models/mac_select.go`. Path
plus an error line is enough; tab completion is not worth the code.

### Consent

Importing writes a private key into `/etc/NetworkManager/system-connections/`
(root-only, 0600) and needs polkit
`org.freedesktop.NetworkManager.settings.modify.system`. Given that this repo
already gates Tor and I2P behind a `confirm` text, the import popup should say
where the key ends up. One line in the popup, not a whole confirmation screen —
the user is the one who chose the file.

---

## Phase 3 — DNS for VPN profiles — **done**

The DNS picker is `PaneKnown`-only. A VPN profile has its own `ipv4.dns`, and a
WireGuard config almost always ships one. Extend the picker to `PaneVPN`.

Landed. `DnsTarget` became `common.DNSTarget`, built from either a
`KnownNetwork` or a `VpnConnection`, which was the refactor this plan expected.
Four things it did not:

- **`SetDnsCmd` ended with `KnownNetworksUpdateMsg`** — the same trap as
  `ToggleAutoConnectCmd` in Phase 1, and the second time this pattern has
  bitten. Split into `writeDNS` plus two commands. The rest of `dbus` was
  audited for a third: `SetMacCmd` and `ToggleHiddenCmd` are the only other
  commands ending that way, and MAC randomisation and hidden-SSID are both
  genuinely wireless-only, so both are correct as they stand.
- **A live tunnel has to be cycled, not re-activated on a device.**
  NetworkManager reads the profile when the tunnel comes up, so nothing else
  rewrites `resolv.conf` for an already-running one. `SetVpnDnsCmd` sequences
  deactivate → activate → refresh.
- **"DHCP" is meaningless inside a tunnel.** The empty option is reworded to
  "None" for a VPN target, via a copy of the provider table — the known
  networks picker shares it and must keep saying DHCP.
- **A DNS column was needed in the VPN table.** Picking a new value for a row
  whose current value is invisible is guesswork. This also covers one of the
  Phase 4 options below.

`d` fits in the VPN pane's status bar where `i` did not — the rendered bar is
exactly 80 columns with nine hints. There is no slack left: a tenth hint, or a
longer label on any existing one, will push pane navigation off the end.

---

## Phase 4 — Say which connection traffic is actually using

From the Wi-Fi note above: two `>` markers and no indication which one carries
traffic. Options, cheapest first:

- Mark the Known Networks row as carrying a tunnel rather than as the exit.
- Show the VPN's DNS in the VPN row, so the Known Networks DNS column is
  obviously not the whole story.
- Reuse the `dnsIsOverridden` idea: compare effective DNS against what the
  Wi-Fi profile asked for, and say so when they disagree.

Worth designing before Phase 5 adds a second way for the same confusion to
arise.

---

## Phase 5 — Provider daemons (Mullvad, Tailscale, Proton)

This is what the revised pane rule buys, and it is the phase with a genuine cost
attached. **Gated on accepting a shell-out.**

The VPN pane grows a second kind of row. `common.VpnConnection` gets a `Kind`,
and the actions dispatch on it:

- **`KindProfile`** — an NM profile. Everything in Phases 1–3.
- **`KindDaemon`** — a vendor daemon.

### Three consequences

**1. Unit-active is not the same as connected.** `mullvad-daemon` runs from boot
and tunnels nothing. So the row's connected state cannot come from systemd. Read
it from **the tunnel device**, via NM's `GetDevices` — device type 29 is
wireguard, 16 is tun — matching interface name to provider from config. That is
provider-agnostic, needs no shell-out, and is true by construction, which is the
same standard `GetSecurityServices` already holds itself to. A `mullvad status`
shell-out would be more precise at the cost of per-provider code.

**2. Connecting needs a command, not a unit start.** Starting the unit does not
connect; `mullvad connect` or `tailscale up` does. Netpala currently does
everything over D-Bus and never shells out, so this is the first break in that.
Config-driven, in the shape of `SecurityServiceConfig` so it reads as familiar:

```toml
[[vpn.providers]]
name       = "Mullvad"
unit       = "mullvad-daemon.service"
interface  = "wg0-mullvad"      # how netpala knows the tunnel is really up
connect    = ["mullvad", "connect"]
disconnect = ["mullvad", "disconnect"]
```

An argv array rather than a shell string, so there is no quoting or injection
surface. It still means netpala runs a command named in a config file, which
should be a deliberate choice rather than a quiet one.

**3. `Remove` stops being uniform.** Deleting an NM profile is
`DeleteConnectionCmd`. "Deleting" Mullvad means uninstalling a package, which
netpala must not do. So `Remove` applies to `KindProfile` rows only — and
`PaneHelp` cannot advertise it for the pane as a whole, since it is currently
keyed on pane alone and would have to become selection-aware.

---

## Deliberately not in scope

- **`.ovpn` import.** NM's importer lives in the `NetworkManager-openvpn`
  plugin, not in the D-Bus API. Doing it properly means reimplementing the
  plugin's option mapping; the alternative is shelling out to
  `nmcli connection import type openvpn file X`, which is ten lines and works
  but abandons the pure-D-Bus design. Better to ship WireGuard well than both
  badly.
- **Server pickers.** Mullvad has roughly 700 endpoints. Importing 700 NM
  profiles is the wrong shape entirely; that needs a provider concept, which is
  a different feature from a VPN manager.
- **Kill switch.** NM has no first-class one. The honest implementation is
  exactly the shape of `contrib/tor-transparent/`: an nftables ruleset dropping
  anything not leaving via the tunnel interface, a systemd unit, and a `confirm`
  text naming what breaks. It would then appear in the **Security** pane as a
  unit — posture, not a provider relationship — which is the pane rule working
  rather than being violated.

---

## Things that will bite

- `mergeWithDefaults` in `config/keybindings.go` is a hand-written if-chain.
  Every new binding means edits in five places: the struct, the defaults, the
  merge, `AppKeyMap`, and `PaneHelp`.
- `PopupState` is a bare int with a comment listing 0–4. Adding 5 is fine, but
  the `switch` in `update()` is already around 220 lines.
- **There is nothing on this machine to test Phase 2 against** — no VPN
  profiles, and no `wg`, `tailscale` or `mullvad` binaries. Either get a real
  config from somewhere or lean hard on unit-testing the parser and the
  settings translation in isolation, and treat the first live import as the
  actual test.

---

## Open questions

1. Is the Phase 5 shell-out acceptable, or should provider daemons wait for
   something better?
2. Is there a WireGuard config available to test Phase 2 against?
3. Does Phase 4 want to be earlier? It is a correctness-of-display issue that
   already exists in a mild form today.
