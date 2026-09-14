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

  ruleset = pkgs.writeText "tor-transparent.nft" (builtins.readFile ./ruleset.nft);

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

    allowedGroup = lib.mkOption {
      type = lib.types.str;
      default = "wheel";
      description = "Group permitted to toggle the unit without a password prompt.";
    };
  };

  config = lib.mkIf cfg.enable {

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

    # Scoped to exactly one unit and one group. This is the entire privilege
    # surface netpala gains -- it cannot manage any other service.
    security.polkit.enable = true;
    security.polkit.extraConfig = ''
      polkit.addRule(function(action, subject) {
        if (action.id == "org.freedesktop.systemd1.manage-units" &&
            action.lookup("unit") == "tor-transparent.service" &&
            subject.isInGroup("${cfg.allowedGroup}")) {
          return polkit.Result.YES;
        }
      });
    '';

    environment.systemPackages = [ pkgs.nftables ];
  };
}
