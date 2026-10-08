package icons

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSiteClientRefusesInternalAddresses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("internal")) }))
	defer srv.Close()
	if _, _, err := fetch(context.Background(), SiteClient(), srv.URL, 100); err == nil {
		t.Fatal("fetching a loopback address should be refused")
	}
	for _, ip := range []string{"127.0.0.1", "10.0.0.5", "172.17.0.2", "192.168.1.1", "169.254.169.254", "100.64.0.1", "::1", "fd00::1", "fe80::1", "0.0.0.0"} {
		if public(net.ParseIP(ip)) {
			t.Errorf("%s counted as public", ip)
		}
	}
	for _, ip := range []string{"1.1.1.1", "140.82.112.3", "2606:4700::1111"} {
		if !public(net.ParseIP(ip)) {
			t.Errorf("%s counted as internal", ip)
		}
	}
}
