package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

func init() {
	// Only override when /etc/resolv.conf does not exist (e.g. Android).
	if _, err := os.Stat("/etc/resolv.conf"); err == nil {
		return
	}

	// DNS servers come from the environment, separated by ";",
	// e.g. KUROMATSU_DNS_SERVER="8.8.8.8:53;1.1.1.1:53;223.5.5.5:53".
	dnsEnv := os.Getenv("KUROMATSU_DNS_SERVER")
	if dnsEnv == "" {
		dnsEnv = "8.8.8.8:53;1.1.1.1:53"
	}

	var dnsServers []string
	for _, s := range strings.Split(dnsEnv, ";") {
		s = strings.TrimSpace(s)
		if s != "" {
			// Add the default :53 port when none is given.
			if _, _, err := net.SplitHostPort(s); err != nil {
				s = s + ":53"
			}
			dnsServers = append(dnsServers, s)
		}
	}

	// Round-robin index across the configured DNS servers.
	var idx uint64

	customResolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{Timeout: 5 * time.Second}
			// Round-robin: try the DNS servers in turn.
			server := dnsServers[atomic.AddUint64(&idx, 1)%uint64(len(dnsServers))]
			return d.DialContext(ctx, "udp", server)
		},
	}

	// Override the global DefaultResolver.
	net.DefaultResolver = customResolver

	// Make http.DefaultTransport dial through the custom DNS resolver.
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Resolver:  customResolver,
	}

	if tr, ok := http.DefaultTransport.(*http.Transport); ok {
		tr.DialContext = dialer.DialContext
	}
}
