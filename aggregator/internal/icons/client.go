package icons

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"syscall"
	"time"
)

// cgnat is carrier-grade NAT space (RFC 6598), which net.IP does not count as private.
var cgnat = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// public reports whether ip is reachable on the internet, not a host-local,
// private or link-local address (cloud metadata lives at 169.254.169.254).
func public(ip net.IP) bool {
	return ip != nil && !(ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() || cgnat.Contains(ip))
}

// SiteClient is the client for fetching sites and their icons. A page decides
// which URLs get fetched, so every connection, redirects included, is checked
// after DNS resolution and refused unless it goes to a public address: no
// requests into the server's own network on a page's say-so.
func SiteClient() *http.Client {
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			if !public(net.ParseIP(host)) {
				return fmt.Errorf("refusing non-public address %s", host)
			}
			return nil
		},
	}
	return &http.Client{
		Timeout: 15 * time.Second,
		// No proxy: the check must see the real destination.
		Transport: &http.Transport{DialContext: dialer.DialContext, TLSHandshakeTimeout: 10 * time.Second, MaxIdleConns: 4},
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			return nil
		},
	}
}
