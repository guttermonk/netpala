package main

import (
	"netpala/common"
	"testing"

	godbus "github.com/godbus/dbus/v5"
)

func twoRadios() NetpalaData {
	m := safeListener(linkModel(false, nil))
	m.DeviceData = []common.Device{
		{Path: godbus.ObjectPath("/dev/wlan0"), Name: "wlan0"},
		{Path: godbus.ObjectPath("/dev/wlan1"), Name: "wlan1"},
	}
	return m
}

// The flag belongs to the device that reported it. A machine with two radios
// is unusual but a dongle alongside a built-in card is not, and only one of
// them is scanning.
func TestOnlyTheReportingDeviceGetsTheScanningFlag(t *testing.T) {
	m := twoRadios()

	next, cmd, handled := m.handleDataMsg(common.ScanningStateMsg{
		DevicePath: godbus.ObjectPath("/dev/wlan1"),
		Scanning:   true,
	})
	if !handled {
		t.Fatal("not handled as a data message")
	}
	if cmd != nil {
		t.Error("handler returned a command; the listener re-arms itself")
	}

	if next.DeviceData[0].Scanning {
		t.Error("wlan0 marked as scanning; it did not report anything")
	}
	if !next.DeviceData[1].Scanning {
		t.Error("wlan1 not marked as scanning although it reported it")
	}
}

// And it clears again, which is the half a re-read used to get wrong: by the
// time the device list came back NetworkManager had finished, so the flag read
// false on the way up as well as on the way down.
func TestTheScanningFlagClears(t *testing.T) {
	m := twoRadios()
	m.DeviceData[0].Scanning = true

	next, _, _ := m.handleDataMsg(common.ScanningStateMsg{
		DevicePath: godbus.ObjectPath("/dev/wlan0"),
		Scanning:   false,
	})

	if next.DeviceData[0].Scanning {
		t.Error("flag still set after the scan finished")
	}
}

// A device that has gone between the signal and its handling leaves the rest
// alone. Device churn is why the signal loop was rewritten in the first place.
func TestAnUnknownDeviceLeavesTheListAlone(t *testing.T) {
	m := twoRadios()

	next, _, handled := m.handleDataMsg(common.ScanningStateMsg{
		DevicePath: godbus.ObjectPath("/dev/gone"),
		Scanning:   true,
	})
	if !handled {
		t.Fatal("not handled as a data message")
	}

	for _, d := range next.DeviceData {
		if d.Scanning {
			t.Errorf("%s marked as scanning by a signal from another device", d.Name)
		}
	}
}
