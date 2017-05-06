package config

import (
	"time"

	"github.com/elastic/beats/libbeat/common"
)

type Config struct {
	Period     time.Duration    `config:"period"`
	Privileged bool             `config:"privileged"`
	UseIPv4    bool             `config:"useipv4"`
	UseIPv6    bool             `config:"useipv6"`
	Targets    []*common.Config `config:"targets"`
}

var DefaultConfig = Config{
	Period:     1 * time.Second,
	Privileged: true,
	UseIPv4:    true,
	UseIPv6:    true,
}
