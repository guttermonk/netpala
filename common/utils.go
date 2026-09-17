package common

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// windowSizeForTest overrides the terminal size when non-zero, so table and
// status-bar layout can be exercised at widths other than whatever the test
// runner happens to have.
var windowSizeForTest struct{ Width, Height int }

func WindowDimensions() struct{ Width, Height int } {
	if windowSizeForTest.Width > 0 {
		return windowSizeForTest
	}
	width, height, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return struct{ Width, Height int }{80, 80}
	}
	return struct{ Width, Height int }{width, height}
}

func freqToBand(freq int) string {
	switch {
	case freq >= 2400 && freq < 2500:
		return "2.4 GHz"
	case freq >= 5000 && freq < 6000:
		return "5 GHz"
	case freq >= 5925 && freq < 7125:
		return "6 GHz"
	default:
		return fmt.Sprintf("%d MHz", freq)
	}
}

func padHeaders(headers []string, headerLengths []int) []string {
	if len(headers) == 0 {
		return headers
	}

	// Fallback: if no lengths provided, auto-fill with -1 (flex)
	if headerLengths == nil || len(headerLengths) != len(headers) {
		headerLengths = make([]int, len(headers))
		for i := range headerLengths {
			headerLengths[i] = -1
		}
	}

	availableWidth := max(WindowDimensions().Width-2, 1)

	// Calculate fixed width and identify flexible columns
	fixedWidth := 0
	flexColumns := []int{} // indices of flexible columns
	for i, w := range headerLengths {
		if w == -1 {
			flexColumns = append(flexColumns, i)
		} else {
			fixedWidth += w
		}
	}

	remaining := max(availableWidth-fixedWidth, 0)

	// Calculate base width and remainder for flexible columns
	flexCount := len(flexColumns)
	baseWidth := 0
	remainder := 0

	if flexCount > 0 {
		baseWidth = remaining / flexCount
		remainder = remaining % flexCount
	}

	// Distribute widths to flexible columns, handling remainder
	flexWidths := make([]int, flexCount)
	for i := range flexWidths {
		flexWidths[i] = baseWidth
		if i < remainder {
			flexWidths[i]++
		}
	}

	// Assign the calculated widths back to headerLengths
	for i, flexIndex := range flexColumns {
		headerLengths[flexIndex] = flexWidths[i]
	}

	// Render headers with their respective widths
	finalHeaders := make([]string, len(headers))
	for i, h := range headers {
		width := max(headerLengths[i], 1)
		finalHeaders[i] = lipgloss.NewStyle().
			Width(width).
			Align(lipgloss.Center).
			Render(h)
	}

	return finalHeaders
}

func CalcTitle(title string, selected bool, primaryColor, activeColor string) string {
	color := primaryColor
	bold := false
	if selected {
		color = activeColor
		bold = true
	}
	width := WindowDimensions().Width
	// Measured in columns, not bytes. Titles now carry profile names, and a
	// single non-ASCII character in one would shorten the rule by two and
	// leave the box border ragged.
	repeatCount := max(width-4-lipgloss.Width(title), 0)
	return lipgloss.NewStyle().
		Bold(bold).
		Foreground(lipgloss.Color(color)).
		Align(lipgloss.Center).
		Render(fmt.Sprintf("┌ %s %s┐", title, strings.Repeat("─", repeatCount)))
}

var BoxBorder = lipgloss.Border{
	Bottom: "─", Left: "│", Right: "│",
	BottomLeft: "└", BottomRight: "┘",
}

func ActiveBorderStyle(color string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color))
}

func InactiveBorderStyle(color string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color))
}

func BoxStyle(selectedRow int, selectedBox bool, height int, primaryColor, activeTextColor, inactiveColor, selectionBg string) func(row, col int) lipgloss.Style {
	return func(row int, col int) lipgloss.Style {
		switch {
		case row == 0:
			return lipgloss.NewStyle().
				Bold(true).
				Foreground(func() lipgloss.Color {
					if selectedBox {
						return lipgloss.Color(activeTextColor)
					}
					return lipgloss.Color(primaryColor)
				}()).
				AlignHorizontal(lipgloss.Center)
		case row == min(selectedRow+2, height+1) && selectedBox:
			return lipgloss.NewStyle().
				Background(lipgloss.Color(selectionBg)).
				Foreground(lipgloss.Color(inactiveColor)).
				AlignHorizontal(lipgloss.Center)
		default:
			return lipgloss.NewStyle().Foreground(lipgloss.Color(primaryColor)).AlignHorizontal(lipgloss.Center)
		}
	}
}

func FormatDeviceData(devices []Device) [][]string {
	data := [][]string{
		padHeaders([]string{"Name", "Mode", "Powered", "State", "Scanning", "Frequency", "Security"}, []int{-1, -1, -1, -1, -1, -1, -1}), {""},
	}
	for _, d := range devices {
		powered := "Off"
		if d.Powered {
			powered = "On"
		}

		row := []string{d.Name, d.Mode, powered, DeviceStateLabel(d.State), strconv.FormatBool(d.Scanning), freqToBand(d.Frequency), d.Security}
		for i := range row {
			if lipgloss.Width(row[i]) > lipgloss.Width(data[0][i]) {
				row[i] = row[i][:max(0, lipgloss.Width(data[0][i])-3)] + "..."
			}
		}

		data = append(data, row)
	}
	return data
}

func FormatStationData(devices []Device) [][]string {
	data := [][]string{
		padHeaders([]string{"State", "Scanning", "Frequency", "Security"}, []int{-1, -1, -1, -1}), {""},
	}
	for _, d := range devices {
		row := []string{DeviceStateLabel(d.State), strconv.FormatBool(d.Scanning), freqToBand(d.Frequency), d.Security}
		for i := range row {
			if lipgloss.Width(row[i]) > lipgloss.Width(data[0][i]) {
				row[i] = row[i][:max(0, lipgloss.Width(data[0][i])-3)] + "..."
			}
		}

		data = append(data, row)
	}
	return data
}

// vpnWidths lays the VPN pane out on the known-networks grid.
//
// Name takes the first third, as it does there, and the four detail columns
// land on the boundaries of the six above them:
//
//	Known:  │ Name │ Security │ DNS │ MAC  │ Hidden │ Auto │ Signal │
//	VPN:    │ Name │    Type      │  Endpoint   │  DNS   │  Auto  │
//
// So Type begins where Security does, Endpoint where MAC does, DNS where Auto
// does and Auto where Signal does. Derived by summing the known widths rather
// than recomputing fractions, so the two cannot drift apart: the remainder
// that cannot be split six ways is distributed unevenly, and any arithmetic
// that ignored that would misalign by a column at most widths.
func vpnWidths() []int {
	k := knownWidths() // marker, Name, Security, DNS, MAC, Hidden, Auto, Signal
	return []int{
		k[0],        // marker
		k[1],        // Name
		k[2] + k[3], // Type       spans Security + DNS
		k[4] + k[5], // Endpoint   spans MAC + Hidden
		k[6],        // DNS        over Auto
		k[7],        // Auto       over Signal
	}
}

func FormatVpnData(vpns []VpnConnection) [][]string {
	data := [][]string{
		padHeaders([]string{"", "Name", "Type", "Endpoint", "DNS", "Auto"}, vpnWidths()), {""},
	}
	for _, vpn := range vpns {
		state := "     "
		if vpn.Connected {
			state = "  >  "
		}

		// A vendor daemon keeps its endpoint, resolvers and reconnect setting
		// inside its own configuration, where netpala cannot read them. "-"
		// says so. Leaving these blank would read as "none", and "None" in
		// the DNS column would be an outright lie -- a provider almost always
		// pins its own.
		endpoint, dns, auto := vpn.Endpoint, VpnDNSLabel(vpn.DNSMode), strconv.FormatBool(vpn.AutoConnect)
		if vpn.Kind == VpnKindDaemon {
			endpoint, dns, auto = "-", "-", "-"
		}

		row := []string{state, vpn.Name, vpn.ConnType, endpoint, dns, auto}
		for i := range row {
			if lipgloss.Width(row[i]) > lipgloss.Width(data[0][i]) {
				row[i] = row[i][:max(0, lipgloss.Width(data[0][i])-3)] + "..."
			}
		}

		data = append(data, row)
	}
	return data
}

// ServiceStateLabel picks the clearest word for a unit's state.
//
// A Type=oneshot unit reports SubState "exited" once it has done its job and
// its process has returned. For something whose whole purpose is to load a
// firewall ruleset and finish, that IS success -- but "exited" reads like a
// crash. Report what ActiveState says the unit is, and only reach for SubState
// when it carries information ActiveState does not.
func ServiceStateLabel(s SecurityService) string {
	switch s.State {
	case "active":
		// "running" for a daemon still resident; "active" covers oneshot
		// units that exited successfully and are held by RemainAfterExit.
		if s.SubState == "running" {
			return "running"
		}
		return "active"
	case "failed":
		return "failed"
	case "inactive":
		return "inactive"
	case "activating":
		return "starting"
	case "deactivating":
		return "stopping"
	}
	if s.SubState != "" {
		return s.SubState
	}
	return s.State
}

// markerWidth is the blank column carrying the ">" that marks a row as live.
const markerWidth = 5

// thirds divides a section into three equal content columns, giving any
// leftover columns to the last one so the row still fills the width exactly.
func thirds() (a, b, c int) {
	total := max(WindowDimensions().Width-2, 3)
	third := total / 3
	return third, third, total - 2*third
}

// firstThirdSplit divides the leading third into the marker column and the
// label column beside it.
//
// Every pane uses this, including the ones with nothing to mark. New Networks
// holds no marker -- a scan result is not connected to anything -- but it
// still reserves the column, because without it that pane's first label starts
// five columns left of every other pane's and the whole view reads as
// misaligned.
//
// The floor is the widest label any pane puts in this column, not each pane's
// own, so that a terminal narrow enough to trigger it does not move one pane's
// boundary without moving the others'.
func firstThirdSplit() (marker, label int) {
	third, _, _ := thirds()
	return markerWidth, max(third-markerWidth, lipgloss.Width("Service"))
}

func FormatSecurityData(services []SecurityService) [][]string {
	// Three equal columns, matching the New Networks pane so the two line up
	// with each other. The marker lives inside the first third rather than
	// beside it, which is what keeps the column boundaries on the same
	// fractions in both panes.
	marker, service := firstThirdSplit()
	_, unit, state := thirds()

	data := [][]string{
		padHeaders([]string{"", "Service", "Unit", "State"},
			[]int{marker, service, unit, state}), {""},
	}
	for _, s := range services {
		marker := "     "
		if s.Active {
			marker = "  >  "
		}

		row := []string{marker, s.Name, s.Unit, ServiceStateLabel(s)}
		for i := range row {
			if lipgloss.Width(row[i]) > lipgloss.Width(data[0][i]) {
				row[i] = row[i][:max(0, lipgloss.Width(data[0][i])-3)] + "..."
			}
		}

		data = append(data, row)
	}
	return data
}

// knownDetailColumns is everything to the right of Name: Security, DNS, MAC,
// Hidden, Auto, Signal.
const knownDetailColumns = 6

// KnownDetailColumnsFitFrom is the narrowest terminal at which every detail
// column in the known-networks table is wide enough for the longest label it
// can hold. Narrower than this and "Cloudflare" is shown as "Cloudf...".
//
// Exported so the tests can assert the threshold rather than assume it: it is
// a consequence of the equal split, and moving either the split or a label
// moves it.
const KnownDetailColumnsFitFrom = 85

// knownWidths gives Name the first third and divides the rest equally.
//
// The marker sits inside Name's third rather than beside it, the same as in the
// Security pane, so the first column boundary lands on the same fraction in
// every pane.
//
// Equal detail columns mean they are all as narrow as the narrowest needs to
// be: at 80 columns they come out at 9, which is not enough for "Cloudflare".
// That is the trade for the even split -- below 85 columns the DNS cell
// truncates rather than the layout bending to fit it. Above 85 everything
// fits, and KnownDetailColumnsFitFrom records where that line is.
func knownWidths() []int {
	total := max(WindowDimensions().Width-2, knownDetailColumns+markerWidth+1)

	marker, name := firstThirdSplit()
	rest := total - marker - name
	base, extra := rest/knownDetailColumns, rest%knownDetailColumns

	widths := make([]int, 0, knownDetailColumns+2)
	widths = append(widths, marker, name)
	for i := range knownDetailColumns {
		w := base
		if i < extra {
			w++ // spread the remainder rather than leaving a ragged edge
		}
		widths = append(widths, w)
	}
	return widths
}

// autoHeader is the auto-connect column's title, in full where the column can
// hold it and shortened where it cannot.
//
// Not a cosmetic choice. padHeaders renders a header inside a style of exactly
// the column's width, and lipgloss *wraps* text that does not fit rather than
// truncating it -- "Auto-Connect" in an 8-wide column becomes two lines. The
// layout budgets one row per network, so a wrapped header pushes the bottom of
// the view off the screen. Shortening is the only option that keeps the row
// count fixed.
//
// "Auto" is what the status bar calls the key, so the short form is not a
// coinage the user has to decode.
func autoHeader() string {
	const full = "Auto-Connect"

	widths := knownWidths()
	if len(widths) < 7 {
		return "Auto"
	}
	if widths[6] >= lipgloss.Width(full) {
		return full
	}
	return "Auto"
}

func FormatKnownNetworksData(networks []KnownNetwork, selectedRow int, height int) [][]string {
	base := [][]string{
		// Name takes the first third; the six detail columns divide the rest
		// equally. See knownWidths and autoHeader.
		padHeaders([]string{"", "Name", "Security", "DNS", "MAC", "Hidden", autoHeader(), "Signal"},
			knownWidths()), {""},
	}
	window := FormatArrays(networks, selectedRow, height)
	for _, n := range window {
		connected := "     "
		if n.Connected {
			connected = "  >  "
		}
		row := []string{connected, strings.TrimSpace(n.SSID), n.Security, DNSModeLabel(n.DNSMode), MACModeLabel(n.MACMode), strconv.FormatBool(n.Hidden), strconv.FormatBool(n.AutoConnect), strconv.Itoa(n.Signal) + "%"}
		for i := range row {
			if len(row[i]) > lipgloss.Width(base[0][i]) {
				row[i] = row[i][:max(0, lipgloss.Width(base[0][i])-3)] + "..."
			}
		}

		base = append(base, row)
	}

	// Pad against the window rather than the full list, so the pane always
	// occupies exactly `height` content rows no matter how many networks are
	// saved. The layout in netpala.go budgets on that guarantee.
	for i := len(window); i < height; i++ {
		base = append(base, []string{""})
	}
	return base
}

func FormatScannedNetworksData(networks []ScannedNetwork, selectedRow int, height int) [][]string {
	// Three equal columns, matching the Security pane. This list is for
	// discovery rather than for acting on, so there is nothing here that
	// deserves the slack the way Name does in the known-networks table.
	//
	// The leading column is always blank: nothing in a scan is connected, so
	// there is never a marker to put in it. It is reserved anyway so Name
	// starts where every other pane's first label does -- see firstThirdSplit.
	marker, name := firstThirdSplit()
	_, security, signal := thirds()

	data := [][]string{
		padHeaders([]string{"", "Name", "Security", "Signal"},
			[]int{marker, name, security, signal}), {""},
	}
	window := FormatArrays(networks, selectedRow, height)
	for _, n := range window {
		row := []string{"", n.SSID, n.Security, strconv.Itoa(n.Signal) + "%"}
		for i := range row {
			if lipgloss.Width(row[i]) > lipgloss.Width(data[0][i]) {
				row[i] = row[i][:max(0, lipgloss.Width(data[0][i])-3)] + "..."
			}
		}

		data = append(data, row)
	}
	for i := len(window); i < height; i++ {
		data = append(data, []string{""})
	}
	return data
}

func FormatArrays[ArrType KnownNetwork | ScannedNetwork](arr []ArrType, selectedIndex int, windowSize int) []ArrType {
	start := 0
	if selectedIndex >= windowSize {
		start = selectedIndex - windowSize + 1
	}
	end := start + windowSize
	if end > len(arr) {
		end = len(arr)
		start = max(end-windowSize, 0)
	}
	if start > end {
		start = end
	}
	return arr[start:end]
}

func CalculatePadding(s string) int {
	totalWidth := WindowDimensions().Width
	line := strings.Split(s, "\n")[0]

	// Use lipgloss.Width to correctly calculate visible width, ignoring ANSI codes
	textWidth := lipgloss.Width(line)

	// Calculate padding and ensure it's not negative
	return max(0, (totalWidth-textWidth)/2)
}

func SanitizeSSID(s, replacement string) string {
	// Unicode regex range for emojis — covers most common sets (Emoticons, Misc Symbols, Transport, etc.)
	re := regexp.MustCompile(`[\p{So}\p{Sk}\p{Cs}\x{1F000}-\x{1FAFF}\x{2600}-\x{27BF}\x{1F300}-\x{1F6FF}]+`)
	return re.ReplaceAllString(s, replacement)
}

func SortDevicesBySignal(devices []ScannedNetwork) {
	slices.SortFunc(devices, func(a, b ScannedNetwork) int {
		// Primary sort: Signal descending (higher is better)
		if a.Signal > b.Signal {
			return -1
		}
		if a.Signal < b.Signal {
			return 1
		}
		// Secondary sort: SSID ascending (case-insensitive)
		return strings.Compare(strings.ToLower(a.SSID), strings.ToLower(b.SSID))
	})
}

// SetWindowSizeForTest overrides the reported terminal size. Tests call it
// with 0 to restore the real one.
func SetWindowSizeForTest(w, h int) {
	windowSizeForTest.Width, windowSizeForTest.Height = w, h
}
