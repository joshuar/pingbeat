package main

import (
	"github.com/joshuar/pingbeat/Godeps/_workspace/src/github.com/elastic/libbeat/logp"
	"github.com/joshuar/pingbeat/Godeps/_workspace/src/github.com/elastic/libbeat/outputs"
	"github.com/joshuar/pingbeat/Godeps/_workspace/src/github.com/elastic/libbeat/publisher"
)

type PingConfig struct {
	Period     *int64
	UseIPv4    *bool
	UseIPv6    *bool
	Privileged *bool
	Targets    *map[string][]string
}

type ConfigSettings struct {
	Input   PingConfig
	Output  map[string]outputs.MothershipConfig
	Logging logp.Logging
	Shipper publisher.ShipperConfig
}

var Config ConfigSettings
