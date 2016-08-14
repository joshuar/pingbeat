package config

type PingConfig struct {
	Period     *string
	UseIPv4    *bool
	UseIPv6    *bool
	Privileged *bool
	Targets    *map[string][]string
	Interface  *string
}

type ConfigSettings struct {
	Input PingConfig
}
