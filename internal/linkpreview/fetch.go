// Package linkpreview fetches bounded public HTTPS metadata without forwarding
// user credentials, cookies or proxy environment settings.
package linkpreview

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

var errUnavailable = errors.New("preview unavailable")
var blocked = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("168.63.129.16/32"), netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("::/96"), netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"), netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2002::/16"),
}

func publicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, prefix := range blocked {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}
func safeURL(raw string) (*url.URL, error) {
	if len(raw) > 4096 {
		return nil, errUnavailable
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return nil, errUnavailable
	}
	u.Fragment = ""
	if ip, err := netip.ParseAddr(u.Hostname()); err == nil && !publicIP(ip) {
		return nil, errUnavailable
	}
	return u, nil
}
func newClient() *http.Client {
	transport := &http.Transport{
		Proxy: nil, MaxIdleConns: 8, MaxIdleConnsPerHost: 2, IdleConnTimeout: 30 * time.Second,
		TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 4 * time.Second,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil || port != "443" {
				return nil, errUnavailable
			}
			ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
			if err != nil || len(ips) == 0 {
				return nil, errUnavailable
			}
			// Reject mixed public/private answers; dial the vetted IP directly so
			// DNS cannot change between validation and establishing the socket.
			for _, ip := range ips {
				if !publicIP(ip) {
					return nil, errUnavailable
				}
			}
			var last error
			for _, ip := range ips {
				conn, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				if err == nil {
					return conn, nil
				}
				last = err
			}
			return nil, last
		},
	}
	return &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		req.Header.Del("Referer")
		if len(via) >= 3 {
			return errUnavailable
		}
		_, err := safeURL(req.URL.String())
		return err
	}}
}
func fetch(ctx context.Context, client *http.Client, raw string, maxBytes int64) ([]byte, string, *url.URL, error) {
	u, err := safeURL(raw)
	if err != nil {
		return nil, "", nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", nil, err
	}
	req.Header.Set("User-Agent", "Voxhold-LinkPreview/2.5 (+https://github.com/LoonEman1/voxhold-backend)")
	req.Header.Set("Accept", "text/html,application/json,image/*;q=0.8")
	response, err := client.Do(req)
	if err != nil {
		return nil, "", nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, "", nil, errUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil || int64(len(data)) > maxBytes {
		return nil, "", nil, errUnavailable
	}
	return data, strings.ToLower(response.Header.Get("Content-Type")), response.Request.URL, nil
}
