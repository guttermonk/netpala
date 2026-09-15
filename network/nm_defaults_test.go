package network

import (
	"os"
	"path/filepath"
	"testing"
)

func withConf(t *testing.T, main string, confd map[string]string) {
	t.Helper()
	dir := t.TempDir()

	origMain, origDir := nmMainConf, nmConfDir
	t.Cleanup(func() { nmMainConf, nmConfDir = origMain, origDir })

	nmMainConf = filepath.Join(dir, "NetworkManager.conf")
	if main == "" {
		nmMainConf = filepath.Join(dir, "absent.conf")
	} else if err := os.WriteFile(nmMainConf, []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}

	nmConfDir = filepath.Join(dir, "conf.d")
	if confd == nil {
		return
	}
	if err := os.Mkdir(nmConfDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range confd {
		if err := os.WriteFile(filepath.Join(nmConfDir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWifiMACDefaultReadsTheConnectionSection(t *testing.T) {
	withConf(t, `[connection]
ethernet.cloned-mac-address=preserve
wifi.bgscan=disabled
wifi.cloned-mac-address=permanent

[device]
wifi.scan-rand-mac-address=false
`, nil)

	if got := WifiMACDefault(); got != "permanent" {
		t.Errorf("WifiMACDefault() = %q, want permanent", got)
	}
}

// NetworkManager.conf(5): "If left unspecified, it defaults to preserve."
func TestWifiMACDefaultFallsBackToPreserve(t *testing.T) {
	withConf(t, "[connection]\nwifi.bgscan=disabled\n", nil)
	if got := WifiMACDefault(); got != "preserve" {
		t.Errorf("WifiMACDefault() = %q, want preserve", got)
	}
}

// A machine where the file cannot be read must not have a default invented for
// it - the picker says "NetworkManager's setting" instead of naming a value
// netpala never actually saw.
func TestWifiMACDefaultUnknownWhenUnreadable(t *testing.T) {
	withConf(t, "", nil)
	if got := WifiMACDefault(); got != "" {
		t.Errorf("WifiMACDefault() = %q, want \"\" for an unreadable config", got)
	}
}

func TestConfDOverridesMainFile(t *testing.T) {
	withConf(t,
		"[connection]\nwifi.cloned-mac-address=permanent\n",
		map[string]string{"99-custom.conf": "[connection]\nwifi.cloned-mac-address=random\n"},
	)
	if got := WifiMACDefault(); got != "random" {
		t.Errorf("WifiMACDefault() = %q, want random from conf.d", got)
	}
}

// conf.d is applied in lexical order, so the higher-numbered file wins.
func TestConfDLexicalOrder(t *testing.T) {
	withConf(t, "[connection]\nwifi.cloned-mac-address=permanent\n", map[string]string{
		"10-early.conf": "[connection]\nwifi.cloned-mac-address=stable\n",
		"90-late.conf":  "[connection]\nwifi.cloned-mac-address=random\n",
	})
	if got := WifiMACDefault(); got != "random" {
		t.Errorf("WifiMACDefault() = %q, want random (90- beats 10-)", got)
	}
}

// The key only counts inside [connection]; the same name lives under [device]
// with a different meaning.
func TestKeyOutsideConnectionSectionIsIgnored(t *testing.T) {
	withConf(t, "[device]\nwifi.cloned-mac-address=random\n", nil)
	if got := WifiMACDefault(); got != "preserve" {
		t.Errorf("WifiMACDefault() = %q; a [device] key must not be read as a connection default", got)
	}
}

func TestIniLookupSkipsCommentsAndBlanks(t *testing.T) {
	data := `
# a comment
; another

[connection]
  wifi.cloned-mac-address = stable
`
	got, ok := iniLookup(data, "connection", "wifi.cloned-mac-address")
	if !ok || got != "stable" {
		t.Errorf("iniLookup = %q, %v; want stable, true", got, ok)
	}
}
