# Transparent Tor proxying as a togglable systemd unit.
#
# Design: the dangerous part (the nftables ruleset) is declarative and
# reviewed here. netpala -- or systemctl, or anything else -- only starts and
# stops one unit. Nothing outside this file needs root or firewall knowledge.
#
# Enabling this module does NOT turn Tor routing on. It installs the rules and
# leaves them dormant. `tor-transparent.service` has no wantedBy, so it starts
# only when something explicitly asks.
#
# Usage in configuration.nix:
#   imports = [ ./tor-transparent.nix ];
#   services.torTransparent.enable = true;
#
# Then:  systemctl start tor-transparent    (route everything through Tor)
#        systemctl stop  tor-transparent    (back to normal)

{
  config,
  lib,
  pkgs,
  ...
}:

let
  cfg = config.services.torTransparent;

  # ruleset.nft exempts tor alone, so it loads and can be reviewed on its own.
  # When directUsers is set, the two skuid matches are widened to a set. The
  # substitution is deliberately narrow: it only rewrites those exact lines, so
  # a change to the ruleset that moves or renames them fails loudly at build
  # time rather than silently dropping the exemption.
  bypassSet = "{ " + lib.concatMapStringsSep ", " (u: ''"${u}"'') ([ "tor" ] ++ cfg.directUsers) + " }";

  rulesetText =
    if cfg.directUsers == [ ] then
      builtins.readFile ./ruleset.nft
    else
      builtins.replaceStrings
        [ ''meta skuid "tor" return'' ''meta skuid "tor" accept'' ]
        [ ''meta skuid ${bypassSet} return'' ''meta skuid ${bypassSet} accept'' ]
        (builtins.readFile ./ruleset.nft);

  ruleset = pkgs.writeText "tor-transparent.nft" rulesetText;

  nft = "${pkgs.nftables}/bin/nft";

  # Tearing down must succeed even if a table is already gone, otherwise a
  # failed stop leaves the rules loaded and the user believes Tor is off.
  teardown = pkgs.writeShellScript "tor-transparent-down" ''
    ${nft} delete table ip tor_trans 2>/dev/null || true
    ${nft} delete table ip6 tor_trans6 2>/dev/null || true
  '';

  # Verify the rules actually loaded. A half-applied ruleset is the one
  # outcome worse than no ruleset, because the UI would report "on".
  bringup = pkgs.writeShellScript "tor-transparent-up" ''
    set -eu
    ${nft} -f ${ruleset}
    ${nft} list table ip tor_trans  > /dev/null
    ${nft} list table ip6 tor_trans6 > /dev/null
  '';
in
{
  options.services.torTransparent = {
    enable = lib.mkEnableOption "transparent Tor proxying (installed dormant, started on demand)";

    stateFile = lib.mkOption {
      type = lib.types.str;
      default = "/var/lib/netpala/tor";
      description = ''
        File recording the last on/off choice, replayed at boot. Contains
        the literal string "on" or "off". Written by netpala; read by
        tor-transparent-restore.service.
      '';
    };

    persistAcrossReboots = lib.mkOption {
      type = lib.types.bool;
      default = true;
      description = ''
        Replay {option}`stateFile` at boot. NixOS cannot use `systemctl
        enable` for this: /etc/systemd/system is a read-only symlink into
        the Nix store, so there is nowhere to write the .wants symlink.
      '';
    };

    directUsers = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [ ];
      example = [ "i2pd" ];
      description = ''
        Users whose traffic bypasses the Tor redirect entirely, in addition to
        tor itself.

        This is a hole in the fail-closed ruleset and is empty by default. The
        case it exists for is another anonymity network: i2pd carries its own
        transport, mostly over UDP, which this ruleset drops, and its TCP would
        otherwise be redirected into Tor - so I2P simply does not work while
        transparent proxying is on unless its user is listed here.

        Adding a user means their traffic leaves directly. For i2pd that
        traffic is I2P-encrypted and only reaches I2P peers, so it is not
        plaintext, but it is identifiable as I2P on the wire. If your threat
        model includes hiding *that you use* an anonymity network, this
        defeats it; if it is anonymising what you do, I2P provides that
        itself and does not benefit from being tunnelled through Tor.
      '';
    };

    allowedGroup = lib.mkOption {
      type = lib.types.str;
      default = "wheel";
      description = "Group permitted to toggle the units without a password prompt.";
    };

    allowedUnits = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [
        "tor-transparent.service"
        "dnscrypt-proxy2.service"
        "i2pd.service"
      ];
      description = ''
        Units that {option}`services.torTransparent.allowedGroup` may start and
        stop without authenticating. This is the entire privilege surface
        netpala gains, so it is an explicit list rather than a wildcard -- it
        must not quietly become "manage any service".

        Keep it in step with the `[[security.services]]` entries in
        `~/.config/netpala/config.toml`: a unit listed there but missing here
        gets a polkit refusal instead of a toggle.
      '';
    };
  };

  config = lib.mkIf cfg.enable {

    # nftables resolves these names when the ruleset loads, and errors on one
    # it cannot find. Listing a user whose service is not enabled would make
    # tor-transparent.service fail to start - so transparent proxying would be
    # off entirely, which is the opposite of what someone editing this list
    # intends. Caught at build time rather than at the next reboot.
    assertions = map (user: {
      assertion = config.users.users ? ${user};
      message = ''
        services.torTransparent.directUsers lists "${user}", but no such user
        is defined. nftables resolves these names when the ruleset loads, so
        tor-transparent.service would fail and transparent Tor proxying would
        be off. Enable the service that creates the user (for i2pd that is
        services.i2pd.enable), or drop it from the list.
      '';
    }) cfg.directUsers;

    services.tor = {
      enable = true;
      client = {
        enable = true;
        # Sets TransPort 127.0.0.1:9040.
        transparentProxy.enable = true;
        # Sets DNSPort 127.0.0.1:9053 and AutomapHostsOnResolve, which is
        # what makes .onion resolution work.
        dns.enable = true;
      };
      settings = {
        # Pinned explicitly because ruleset.nft hardcodes this range. If you
        # change one you must change the other, or .onion breaks.
        VirtualAddrNetworkIPv4 = "10.192.0.0/10";
      };
    };

    # netpala writes the toggle here. Group-writable to the same group the
    # polkit rule trusts -- widening one without the other achieves nothing.
    systemd.tmpfiles.rules = [
      "d /var/lib/netpala 0775 root ${cfg.allowedGroup} -"
    ];

    # The switch itself.
    systemd.services.tor-transparent = {
      description = "Transparent Tor proxying (nftables)";
      documentation = [ "file://${ruleset}" ];

      # Ordering only. Deliberately NOT `requires`: if tor dies we want the
      # rules to stay up and keep dropping, not unload and start leaking.
      after = [ "tor.service" "network.target" ];
      wants = [ "tor.service" ];

      # No wantedBy: dormant until something starts it.

      serviceConfig = {
        Type = "oneshot";
        RemainAfterExit = true;
        ExecStart = "${bringup}";
        ExecStop = "${teardown}";
      };
    };

    systemd.services.tor-transparent-restore = lib.mkIf cfg.persistAcrossReboots {
      description = "Replay the last Tor transparent-proxy choice";
      wantedBy = [ "multi-user.target" ];
      after = [ "tor.service" "network-online.target" ];
      wants = [ "network-online.target" ];
      serviceConfig = {
        Type = "oneshot";
        RemainAfterExit = true;
      };
      script = ''
        if [ "$(cat ${cfg.stateFile} 2>/dev/null || echo off)" = "on" ]; then
          systemctl start tor-transparent.service
        fi
      '';
    };

    # Scoped to a fixed list of units and one group. This is the entire
    # privilege surface netpala gains -- it cannot manage anything else.
    # A list rather than a wildcard, deliberately.
    security.polkit.enable = true;
    security.polkit.extraConfig = ''
      polkit.addRule(function(action, subject) {
        var allowed = ${builtins.toJSON cfg.allowedUnits};
        if (action.id == "org.freedesktop.systemd1.manage-units" &&
            allowed.indexOf(action.lookup("unit")) !== -1 &&
            subject.isInGroup("${cfg.allowedGroup}")) {
          return polkit.Result.YES;
        }
      });
    '';

    environment.systemPackages = [ pkgs.nftables ];
  };
}
