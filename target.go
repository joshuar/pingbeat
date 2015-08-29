package main

import (
	"github.com/elastic/libbeat/logp"
	"net"
)

type Target struct {
	name      string
	tag       string
	ipv4Addrs []net.IPAddr
	ipv6Addrs []net.IPAddr
}

func (t *Target) Init(name string, tag string) {
	t.name = name
	t.tag = tag
	logp.Debug("pingbeat", "Getting IP addresses for %s:\n", t.name)
	addrs, err := net.LookupIP(t.name)
	if err != nil {
		logp.Err("pingbeat", "Failed to resolve %s to IP address\n", t.name)
	}
	for j := 0; j < len(addrs); j++ {
		if addrs[j].To4() != nil {
			logp.Debug("pingbeat", "IPv4: %s\n", addrs[j].String())
			t.ipv4Addrs = append(t.ipv4Addrs, net.IPAddr{IP: addrs[j]})
		} else {
			logp.Debug("pingbeat", "IPv6: %s\n", addrs[j].String())
			t.ipv6Addrs = append(t.ipv6Addrs, net.IPAddr{IP: addrs[j]})
		}
	}
}
