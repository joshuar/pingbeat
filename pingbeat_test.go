package main

import (
	"github.com/stretchr/testify/assert"
	"net"
	"testing"
)

func TestAddTarget(t *testing.T) {
	pingbeat := Pingbeat{}
	pingbeat.ipv4targets = make(map[string][2]string)
	pingbeat.ipv6targets = make(map[string][2]string)

	// test adding target as IP address
	name := "192.168.1.1"
	addr := name
	tag := "target_as_addr"
	pingbeat.AddTarget(name, tag)
	assert.Equal(t, name, pingbeat.ipv4targets[addr][0])
	assert.Equal(t, tag, pingbeat.ipv4targets[addr][1])

	// test adding target as a DNS name
	name = "elastic.co"
	tag = "target_as_name"
	addrs, err := net.LookupIP(name)
	if err != nil {
		t.Logf("Failed to resolve %s to IP address!", name)
		t.Fail()
	} else {
		addr := addrs[0].String()
		pingbeat.AddTarget(name, tag)
		assert.Equal(t, name, pingbeat.ipv4targets[addr][0])
		assert.Equal(t, tag, pingbeat.ipv4targets[addr][1])
	}
}

func TestAddr2Name(t *testing.T) {
	pingbeat := Pingbeat{}
	pingbeat.ipv4targets = make(map[string][2]string)
	pingbeat.ipv6targets = make(map[string][2]string)

	addrs, err := net.ResolveIPAddr("ip", "127.0.0.1")
	if err != nil {
		t.Logf("Failed to resolve 127.0.0.1 to DNS name!")
		t.Fail()
	} else {
		pingbeat.AddTarget(addrs.IP.String(), "addr2name")
	}
	// test lookup of address works
	name, tag := pingbeat.Addr2Name(addrs)
	assert.Equal(t, name, addrs.IP.String())
	assert.Equal(t, tag, "addr2name")

	// test lookup returns err for nonexistent address
	addrs = &net.IPAddr{IP: net.ParseIP("192.168.1.1"), Zone: ""}
	name, tag = pingbeat.Addr2Name(addrs)
	assert.Equal(t, name, "err")
	assert.Equal(t, tag, "err")
}
