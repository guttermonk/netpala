package models

import (
	"fmt"
	"math"
	"netpala/config"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
)

// bubblesDefaultPlaceholder is the style a textinput carries when nothing has
// set one: ANSI 240, a dark grey that disappears against a dark or mid-grey
// terminal background. Nothing in netpala may be left on it.
const bubblesDefaultPlaceholder = "240"

func placeholderColour(in textinput.Model) string {
	return fmt.Sprint(in.PlaceholderStyle.GetForeground())
}

// Every input in the program. The failure this guards against is silent -- an
// input simply never gets styled and looks fine to whoever added it, because
// it depends on the reader's terminal background.
func allInputs(colors config.Colors) map[string]textinput.Model {
	keys := config.DefaultKeyBindings()

	eap := ModelWpaEapForm(colors)
	mac := ModelMacSelect(colors, keys, "")
	dns := ModelDnsSelect(colors, keys, nil)

	return map[string]textinput.Model{
		"password":     ModelPasswordInput(colors).Password,
		"vpn import":   ModelVpnImport(colors, keys).Path,
		"mac explicit": mac.Explicit,
		"dns custom":   dns.Custom,
		"eap identity": eap.Identity,
		"eap password": eap.Password,
		"eap ca cert":  eap.CaCert,
	}
}

func TestEveryInputUsesTheConfiguredPlaceholderColour(t *testing.T) {
	colors := config.DefaultColors()
	colors.Placeholder = "#ff00ff" // unmistakable

	for name, in := range allInputs(colors) {
		if got := placeholderColour(in); got != "#ff00ff" {
			t.Errorf("%s: placeholder colour is %q, want the configured one", name, got)
		}
	}
}

func TestNoInputIsLeftOnTheBubblesDefault(t *testing.T) {
	for name, in := range allInputs(config.DefaultColors()) {
		if got := placeholderColour(in); got == bubblesDefaultPlaceholder {
			t.Errorf("%s: still on the bubbles default placeholder colour", name)
		}
	}
}

// Every input still has something to show, or there is nothing to style.
func TestEveryInputHasAPlaceholder(t *testing.T) {
	for name, in := range allInputs(config.DefaultColors()) {
		if strings.TrimSpace(in.Placeholder) == "" {
			t.Errorf("%s: no placeholder text", name)
		}
	}
}

// Dim enough to read as "not what you typed", but not the same as real input.
func TestPlaceholderIsDistinctFromTypedText(t *testing.T) {
	colors := config.DefaultColors()
	in := ModelVpnImport(colors, config.DefaultKeyBindings()).Path

	placeholder := fmt.Sprint(in.PlaceholderStyle.GetForeground())
	typed := fmt.Sprint(in.TextStyle.GetForeground())

	if placeholder == typed {
		t.Errorf("placeholder and typed text are both %q; the hint would read as input", typed)
	}
	if placeholder != colors.Placeholder {
		t.Errorf("placeholder colour = %q, want %q", placeholder, colors.Placeholder)
	}
	if typed != colors.Primary {
		t.Errorf("typed text colour = %q, want the primary colour %q", typed, colors.Primary)
	}
}

// The default has to be light enough to see. The bubbles default fails this,
// which is the bug that started it.
func TestDefaultPlaceholderIsLegible(t *testing.T) {
	c, err := parseHex(config.DefaultColors().Placeholder)
	if err != nil {
		t.Fatalf("default placeholder colour is not a hex value: %v", err)
	}
	if got := relativeLuminance(c); got < 0.20 {
		t.Errorf("default placeholder luminance is %.2f, too dark to read on a grey background", got)
	}

	// And still dimmer than ordinary text, or it stops reading as a hint.
	primary, err := parseHex(config.DefaultColors().Primary)
	if err != nil {
		t.Fatal(err)
	}
	if relativeLuminance(c) >= relativeLuminance(primary) {
		t.Error("the placeholder is no dimmer than typed text")
	}
}

type rgb struct{ R, G, B float64 }

func parseHex(s string) (rgb, error) {
	var r, g, b int
	if _, err := fmt.Sscanf(strings.TrimPrefix(s, "#"), "%02x%02x%02x", &r, &g, &b); err != nil {
		return rgb{}, err
	}
	return rgb{float64(r) / 255, float64(g) / 255, float64(b) / 255}, nil
}

// relativeLuminance is the sRGB formula, enough to tell "readable" from
// "invisible" without pulling in a colour library.
func relativeLuminance(c rgb) float64 {
	lin := func(v float64) float64 {
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(c.R) + 0.7152*lin(c.G) + 0.0722*lin(c.B)
}
