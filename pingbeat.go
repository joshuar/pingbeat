package main

import (
	// "github.com/davecgh/go-spew/spew"
	"github.com/elastic/libbeat/common"
	"github.com/elastic/libbeat/logp"
	"github.com/tatsushid/go-fastping"
	"net"
	"os"
	"time"
)

type Pingbeat struct {
	isAlive     bool
	useIPv4     bool
	useIPv6     bool
	period      time.Duration
	pingType    string
	ipv4targets map[string][2]string
	ipv6targets map[string][2]string
	events      chan common.MapStr
}

func (p *Pingbeat) Init(config PingConfig, events chan common.MapStr) error {

	if config.Period != nil {
		p.period = time.Duration(*config.Period) * time.Second
	} else {
		p.period = 1 * time.Second
	}
	logp.Debug("pingbeat", "Period %v\n", p.period)

	if *config.Privileged {
		p.pingType = "ip"
	} else {
		p.pingType = "udp"
	}
	logp.Debug("pingbeat", "Using %v for pings\n", p.pingType)

	p.useIPv4 = true
	p.useIPv6 = false
	if config.UseIPv4 != nil && *config.UseIPv4 == false {
		p.useIPv4 = false
	}
	if config.UseIPv6 != nil && *config.UseIPv6 == true {
		p.useIPv6 = true
	}
	logp.Debug("pingbeat", "IPv4: %v, IPv6: %v\n", p.useIPv4, p.useIPv6)

	p.ipv4targets = make(map[string][2]string)
	p.ipv6targets = make(map[string][2]string)
	if config.Targets != nil {
		for tag, targets := range *config.Targets {
			for i := 0; i < len(targets); i++ {
				logp.Debug("pingbeat", "Getting IP addresses for %s:\n", targets[i])
				addrs, err := net.LookupIP(targets[i])
				if err != nil {
					logp.Warn("Failed to resolve %s to IP address, ignoring this target.\n", targets[i])
				} else {
					for j := 0; j < len(addrs); j++ {
						if addrs[j].To4() != nil {
							logp.Debug("pingbeat", "IPv4: %s\n", addrs[j].String())
							p.ipv4targets[addrs[j].String()] = [2]string{targets[i], tag}
						} else {
							logp.Debug("pingbeat", "IPv6: %s\n", addrs[j].String())
							p.ipv6targets[addrs[j].String()] = [2]string{targets[i], tag}
						}
					}
				}
			}
		}
	}

	p.events = events
	return nil
}

func (p *Pingbeat) Run() error {

	p.isAlive = true

	fp := fastping.NewPinger()

	errInput, err := fp.Network(p.pingType)
	if err != nil {
		logp.Critical("Error: %v (input %v)", err, errInput)
		os.Exit(1)
	}

	if p.useIPv4 {
		for addr, details := range p.ipv4targets {
			logp.Debug("pingbeat", "Adding target IP: %s, Name: %s, Tag: %s\n", addr, details[0], details[1])
			fp.AddIP(addr)
		}
	}
	if p.useIPv6 {
		for addr, details := range p.ipv6targets {
			logp.Debug("pingbeat", "Adding target IP: %s, Name: %s, Tag: %s\n", addr, details[0], details[1])
			fp.AddIP(addr)
		}
	}
	fp.OnRecv = func(addr *net.IPAddr, rtt time.Duration) {
		var name, tag string
		ip := addr.IP
		if ip.To4() != nil {
			name = p.ipv4targets[addr.String()][0]
			tag = p.ipv4targets[addr.String()][1]
		} else {
			name = p.ipv6targets[addr.String()][0]
			tag = p.ipv6targets[addr.String()][1]
		}
		event := common.MapStr{
			"timestamp":   common.Time(time.Now()),
			"type":        "pingbeat",
			"target_name": name,
			"target_addr": addr.String(),
			"tag":         tag,
			"rtt":         milliSeconds(rtt),
		}
		p.events <- event
	}
	// fp.OnIdle = func() {
	// 	fmt.Println("loop done")
	// }
	for p.isAlive {
		time.Sleep(p.period)
		err := fp.Run()
		if err != nil {
			logp.Warn("Warning: %v", err)
		}
	}

	return nil
}

func (p *Pingbeat) Stop() {
	p.isAlive = false
}
