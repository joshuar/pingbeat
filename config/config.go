package config

import (
	"github.com/elastic/beats/libbeat/common"
	"time"
)

type Config struct {
	Period     time.Duration    `config:"period"`
	Timeout    time.Duration    `config:"timeout"`
	Privileged bool             `config:"privileged"`
	UseIPv4    bool             `config:"useipv4"`
	UseIPv6    bool             `config:"useipv6"`
	Targets    []*common.Config `config:"targets"`
}

var DefaultConfig = Config{
	Period:     1 * time.Second,
	Timeout:    10 * time.Second,
	Privileged: true,
	UseIPv4:    true,
	UseIPv6:    true,
}
