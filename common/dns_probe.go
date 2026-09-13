package common

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"
)

// DNSProbeTimeout bounds a resolver liveness check. Short enough that the
// picker does not feel stalled, long enough for a loopback round trip.
const DNSProbeTimeout = 900 * time.Millisecond

// probeName is in the reserved .invalid TLD, so a healthy resolver answers
// NXDOMAIN without anything leaving the machine. Any answer at all proves it
// is listening; only a timeout or a refused connection means it is not.
const probeName = "netpala-probe.invalid"

// ProbeResolver reports whether a DNS server is answering on port 53. It is
// used before pointing a profile at a local DNSCrypt proxy, because selecting
// a proxy that is not running takes out name resolution entirely.
func ProbeResolver(addr string) error {
	ip := net.ParseIP(addr)
	if ip == nil {
		return fmt.Errorf("%q is not a valid IP address", addr)
	}

	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: DNSProbeTimeout}
			return d.DialContext(ctx, network, net.JoinHostPort(addr, "53"))
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), DNSProbeTimeout)
	defer cancel()

	_, err := resolver.LookupHost(ctx, probeName)
	if err == nil {
		return nil
	}

	// NXDOMAIN is a response, which is exactly what we are looking for.
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
		return nil
	}
	return fmt.Errorf("no response from %s:53", addr)
}

// ProbeResolvers checks every address and returns the first failure. All of
// them have to answer, since resolv.conf will hand queries to any of them.
func ProbeResolvers(addrs []string) error {
	if len(addrs) == 0 {
		return fmt.Errorf("no DNSCrypt address configured")
	}
	for _, a := range addrs {
		if err := ProbeResolver(a); err != nil {
			return err
		}
	}
	return nil
}
