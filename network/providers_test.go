package network

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeSysfs builds a /sys/class/net stand-in with the given interfaces and
// their flag words.
func fakeSysfs(t *testing.T, ifaces map[string]string) {
	t.Helper()
	dir := t.TempDir()
	for name, flags := range ifaces {
		if err := os.MkdirAll(filepath.Join(dir, name), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name, "flags"), []byte(flags), 0644); err != nil {
			t.Fatal(err)
		}
	}

	old := sysClassNet
	sysClassNet = dir
	t.Cleanup(func() { sysClassNet = old })
}

// A daemon row's "connected" marker rests entirely on this, so the reading has
// to be right for the shapes the kernel actually writes.
func TestInterfaceIsUp(t *testing.T) {
	fakeSysfs(t, map[string]string{
		// Real values read off a running machine.
		"wlp3s0": "0x1003\n", // up, broadcast, multicast
		"lo":     "0x9\n",    // up, loopback
		"wg0":    "0x1\n",    // up, nothing else
		"down0":  "0x1002\n", // broadcast+multicast, IFF_UP clear
		"junk0":  "not-a-number\n",
	})

	for name, want := range map[string]bool{
		"wlp3s0": true,
		"lo":     true,
		"wg0":    true,
		"down0":  false,
		"junk0":  false,
		"absent": false,
	} {
		if got := InterfaceIsUp(name); got != want {
			t.Errorf("InterfaceIsUp(%q) = %v, want %v", name, got, want)
		}
	}
}

// The interface name comes from a configuration file, so it must not be able
// to address anything outside sysfs.
func TestInterfaceIsUpRefusesPathsOutsideSysfs(t *testing.T) {
	fakeSysfs(t, map[string]string{"wg0": "0x1\n"})

	for _, name := range []string{
		"",
		"../../../etc",
		"wg0/../wg0",
		"/etc/passwd",
		"wg0\x00",
	} {
		if InterfaceIsUp(name) {
			t.Errorf("InterfaceIsUp(%q) accepted a path that is not a bare interface name", name)
		}
	}
}

// Whitespace and a missing 0x prefix both show up across kernel versions.
func TestInterfaceIsUpToleratesFormatting(t *testing.T) {
	fakeSysfs(t, map[string]string{
		"a": "0x1003",    // no trailing newline
		"b": "1003\n",    // no 0x prefix
		"c": "  0x1  \n", // padded
	})

	for _, name := range []string{"a", "b", "c"} {
		if !InterfaceIsUp(name) {
			t.Errorf("InterfaceIsUp(%q) = false, want true", name)
		}
	}
}
