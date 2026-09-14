package common

// Device connection states shown in the State column.
//
// NetworkManager reports thirteen NM_DEVICE_STATE values; these collapse them
// to the distinctions a user actually needs. The negative values all mean "not
// connected" and differ only in why, so anything testing for "is it up?" can
// compare against DeviceStateConnected.
const (
	DeviceStateUnknown      = -5
	DeviceStateUnmanaged    = -4
	DeviceStateUnavailable  = -3 // exists but unusable: radio off, no carrier, rfkill
	DeviceStateFailed       = -2
	DeviceStateDisconnected = -1
	DeviceStateConnecting   = 0
	DeviceStateConnected    = 1
)

// DeviceStateLabel is the word shown in the State column.
//
// "unavailable" and "failed" are kept distinct from "disconnected" on purpose:
// a radio that is switched off and a connection that just failed are both
// "not connected", but only one of them is something the user did.
func DeviceStateLabel(state int) string {
	switch state {
	case DeviceStateConnected:
		return "connected"
	case DeviceStateConnecting:
		return "connecting"
	case DeviceStateDisconnected:
		return "disconnected"
	case DeviceStateFailed:
		return "failed"
	case DeviceStateUnavailable:
		return "unavailable"
	case DeviceStateUnmanaged:
		return "unmanaged"
	}
	return "unknown"
}
