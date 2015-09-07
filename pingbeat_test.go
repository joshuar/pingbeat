package main

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"net"
	"testing"
)

func TestAddTarget(t *testing.T) {

	var name, addr, tag string

	pingbeat := Pingbeat{}
	pingbeat.ipv4targets = make(map[string][2]string)
	pingbeat.ipv6targets = make(map[string][2]string)

	// test adding target as IP address
	name = "192.168.1.1"
	addr = name
	tag = "target_as_addr"
	pingbeat.AddTarget(name, tag)
	assert.Equal(t, name, pingbeat.ipv4targets[addr][0])
	assert.Equal(t, tag, pingbeat.ipv4targets[addr][1])

	// test adding target as a DNS name
	name = "elastic.co"
	tag = "target_as_name"
	addrs, err := net.LookupIP(name)
	if err != nil {
		fmt.Printf("Failed to resolve %s to IP address!", name)
	} else {
		addr = addrs[0].String()
		pingbeat.AddTarget(name, tag)
		assert.Equal(t, name, pingbeat.ipv4targets[addr][0])
		assert.Equal(t, tag, pingbeat.ipv4targets[addr][1])
	}

}
