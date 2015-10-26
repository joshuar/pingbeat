package main

import (
	// "github.com/davecgh/go-spew/spew"
	"github.com/elastic/libbeat/beat"
	"github.com/elastic/libbeat/cfgfile"
	"github.com/elastic/libbeat/common"
	"github.com/elastic/libbeat/logp"
	"github.com/elastic/libbeat/publisher"
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
	config      ConfigSettings
	events      publisher.Client
}

func milliSeconds(d time.Duration) float64 {
	msec := d / time.Millisecond
	nsec := d % time.Millisecond
	return float64(msec) + float64(nsec)*1e-6
}

func (p *Pingbeat) AddTarget(target string, tag string) {
	addr := net.ParseIP(target)
	if addr != nil {
		if addr.To4() != nil {
			logp.Debug("pingbeat", "IPv4: %s\n", addr.String())
			p.ipv4targets[addr.String()] = [2]string{target, tag}
		} else {
			logp.Debug("pingbeat", "IPv6: %s\n", addr.String())
			p.ipv6targets[addr.String()] = [2]string{target, tag}
		}
	} else {
		logp.Debug("pingbeat", "Getting IP addresses for %s:\n", target)
		addrs, err := net.LookupIP(target)
		if err != nil {
			logp.Warn("Failed to resolve %s to IP address, ignoring this target.\n", target)
		} else {
			for j := 0; j < len(addrs); j++ {
				if addrs[j].To4() != nil {
					logp.Debug("pingbeat", "IPv4: %s\n", addrs[j].String())
					p.ipv4targets[addrs[j].String()] = [2]string{target, tag}
				} else {
					logp.Debug("pingbeat", "IPv6: %s\n", addrs[j].String())
					p.ipv6targets[addrs[j].String()] = [2]string{target, tag}
				}
			}
		}
	}
}

func (p *Pingbeat) Config(b *beat.Beat) error {
	err := cfgfile.Read(&p.config, "")
	if err != nil {
		logp.Err("Error reading configuration file: %v", err)
		return err
	}

	if p.config.Input.Period != nil {
		p.period = time.Duration(*p.config.Input.Period) * time.Second
	} else {
		p.period = 1 * time.Second
	}
	logp.Debug("pingbeat", "Period %v\n", p.period)

	if *p.config.Input.Privileged {
		p.pingType = "ip"
	} else {
		p.pingType = "udp"
	}
	logp.Debug("pingbeat", "Using %v for pings\n", p.pingType)

	if &p.config.Input.UseIPv4 != nil {
		p.useIPv4 = *p.config.Input.UseIPv4
	} else {
		p.useIPv4 = true
	}
	if &p.config.Input.UseIPv6 != nil {
		p.useIPv6 = *p.config.Input.UseIPv6
	} else {
		p.useIPv6 = false
	}
	logp.Debug("pingbeat", "IPv4: %v, IPv6: %v\n", p.useIPv4, p.useIPv6)

	p.ipv4targets = make(map[string][2]string)
	p.ipv6targets = make(map[string][2]string)
	if p.config.Input.Targets != nil {
		for tag, targets := range *p.config.Input.Targets {
			for i := 0; i < len(targets); i++ {
				p.AddTarget(targets[i], tag)
			}
		}
	} else {
		logp.Critical("Error: no targets specified, cannot continue!")
		os.Exit(1)
	}

	return nil
}

func (p *Pingbeat) Setup(b *beat.Beat) error {
	p.events = b.Events
	return nil
}

func (p *Pingbeat) Run(b *beat.Beat) error {
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
		p.events.PublishEvent(event)
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

func (p *Pingbeat) Cleanup(b *beat.Beat) error {
	return nil
}

func (p *Pingbeat) Stop() {
	p.isAlive = false
}
