package network

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// nmMainConf and nmConfDir are where NetworkManager reads global connection
// defaults from. Values in conf.d override the main file, and within conf.d
// the last file wins in lexical order - NetworkManager's own precedence.
var (
	nmMainConf = "/etc/NetworkManager/NetworkManager.conf"
	nmConfDir  = "/etc/NetworkManager/conf.d"
)

// WifiMACDefault reports the behaviour the "Default" MAC mode resolves to on
// this machine.
//
// "Default" means "write nothing to the profile and let NetworkManager
// decide", which is the wifi.cloned-mac-address global default rather than any
// fixed behaviour. It lives in NetworkManager.conf, so without reading it the
// picker can only say "NetworkManager's setting" - true, but it tells the user
// nothing about what will happen to their address, which is the one thing they
// opened the picker to find out.
//
// Returns "" when no configuration file could be read at all, so the caller
// falls back to the vaguer wording rather than asserting a default it did not
// actually verify.
func WifiMACDefault() string {
	value, read := nmConnectionDefault("wifi.cloned-mac-address")
	if !read {
		return ""
	}
	if value == "" {
		// NetworkManager.conf(5): "If left unspecified, it defaults to
		// preserve."
		return "preserve"
	}
	return value
}

// nmConnectionDefault reads one key from the [connection] section, reporting
// whether any configuration file was readable at all.
func nmConnectionDefault(key string) (value string, read bool) {
	for _, path := range nmConfFiles() {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		read = true
		if v, ok := iniLookup(string(data), "connection", key); ok {
			value = v // later files override earlier ones
		}
	}
	return value, read
}

func nmConfFiles() []string {
	files := []string{nmMainConf}
	entries, err := os.ReadDir(nmConfDir)
	if err != nil {
		return files
	}
	var extra []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".conf") {
			extra = append(extra, filepath.Join(nmConfDir, e.Name()))
		}
	}
	sort.Strings(extra)
	return append(files, extra...)
}

// iniLookup finds key within section. Deliberately minimal: these files are
// plain INI and pulling in a parser for one lookup is not worth the dependency.
func iniLookup(data, section, key string) (string, bool) {
	var current string
	var found string
	var ok bool

	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			current = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		if current != section {
			continue
		}
		name, value, isPair := strings.Cut(line, "=")
		if !isPair {
			continue
		}
		if strings.TrimSpace(name) == key {
			// Keep scanning: a repeated key within one file takes its last
			// value, which is what NetworkManager does.
			found, ok = strings.TrimSpace(value), true
		}
	}
	return found, ok
}
