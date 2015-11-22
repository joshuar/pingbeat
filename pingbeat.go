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

// Pingbeat struct contains all options and
// hosts to ping
type Pingbeat struct {
	isAlive     bool
	useIPv4     bool
	useIPv6     bool
	period      time.Duration
	timeout     time.Duration
	pingType    string
	ipv4targets map[string][2]string
	ipv6targets map[string][2]string
	config      ConfigSettings
	events      publisher.Client
	done        chan struct{}
}

// Function to convert s to ms
// Not sure why this isn't provided in time...
func milliSeconds(d time.Duration) float64 {
	msec := d / time.Millisecond
	nsec := d % time.Millisecond
	return float64(msec) + float64(nsec)*1e-6
}

// Simple struct that is filled with ICMP response
// and passed through channels
type response struct {
	name string
	addr *net.IPAddr
	rtt  time.Duration
	tag  string
}

func (p *Pingbeat) AddTarget(target string, tag string) {
	if addr := net.ParseIP(target); addr.String() == "" {
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

func (p *Pingbeat) Addr2Name(addr *net.IPAddr) (string, string) {
	var name, tag string
	if _, found := p.ipv4targets[addr.String()]; found {
		name = p.ipv4targets[addr.String()][0]
		tag = p.ipv4targets[addr.String()][1]
	} else if _, found := p.ipv6targets[addr.String()]; found {
		name = p.ipv6targets[addr.String()][0]
		tag = p.ipv6targets[addr.String()][1]
	} else {
		logp.Err("Error: %s not found in Pingbeat targets!", addr.String())
		name = "err"
		tag = "err"
	}
	return name, tag
}

func (p *Pingbeat) Config(b *beat.Beat) error {
	// Read in provided config file, bail if problem
	err := cfgfile.Read(&p.config, "")
	if err != nil {
		logp.Err("Error reading configuration file: %v", err)
		return err
	}

	// Use period provided in config or default to 5s
	if p.config.Input.Period != nil {
		p.period = time.Duration(*p.config.Input.Period) * time.Second
	} else {
		p.period = 5 * time.Second
	}
	logp.Debug("pingbeat", "Period %v\n", p.period)

	// Check if we can use privileged (i.e. raw socket) ping,
	// else use a UDP ping
	if *p.config.Input.Privileged {
		p.pingType = "ip"
	} else {
		p.pingType = "udp"
	}
	logp.Debug("pingbeat", "Using %v for pings\n", p.pingType)

	// Check whether IPv4/IPv6 pings are requested in config
	// Default to just IPv4 pings
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

	// Fill the IPv4/IPv6 targets maps
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
	p.done = make(chan struct{})
	return nil
}

func (p *Pingbeat) Run(b *beat.Beat) error {

	fp := fastping.NewPinger()

	// Set the MaxRTT, i.e., the interval between pinging a target
	fp.MaxRTT = p.period

	errInput, err := fp.Network(p.pingType)
	results := make(map[string]*response)
	if err != nil {
		logp.Critical("Error: %v (input %v)", err, errInput)
		os.Exit(1)
	}

	if p.useIPv4 {
		for addr, details := range p.ipv4targets {
			logp.Debug("pingbeat", "Adding target IP: %s, Name: %s, Tag: %s\n", addr, details[0], details[1])
			fp.AddIP(addr)
			results[addr] = nil
		}
	}
	if p.useIPv6 {
		for addr, details := range p.ipv6targets {
			logp.Debug("pingbeat", "Adding target IP: %s, Name: %s, Tag: %s\n", addr, details[0], details[1])
			fp.AddIP(addr)
			results[addr] = nil
		}
	}

	onRecv, onIdle := make(chan *response), make(chan bool)
	fp.OnRecv = func(addr *net.IPAddr, t time.Duration) {
		// Find the name and tag associated with this address
		name, tag := p.Addr2Name(addr)
		// Send the name, addr, rtt and tag as a struct through the onRecv channel
		onRecv <- &response{name: name, addr: addr, rtt: t, tag: tag}
	}
	fp.OnIdle = func() {
		onIdle <- true
	}

	fp.RunLoop()

	for {
		select {
		case <-p.done:
			logp.Debug("pingbeat", "Got interrupted, shutting down\n")
			fp.Stop()
			return nil
		case res := <-onRecv:
			results[res.addr.String()] = res
		case <-onIdle:
			for target, r := range results {
				if r == nil {
					var name, tag string
					if _, found := p.ipv4targets[target]; found {
						name = p.ipv4targets[target][0]
						tag = p.ipv4targets[target][1]
					} else if _, found := p.ipv6targets[target]; found {
						name = p.ipv6targets[target][0]
						tag = p.ipv6targets[target][1]
					} else {
						logp.Warn("Error: Unexpected target returned: %s", target)
						break
					}
					// Packet loss
					event := common.MapStr{
						"@timestamp":  common.Time(time.Now()),
						"type":        "pingbeat",
						"target_name": name,
						"target_addr": target,
						"tag":         tag,
						"loss":        true,
					}
					p.events.PublishEvent(event)
				} else {
					// Success, ping received
					event := common.MapStr{
						"@timestamp":  common.Time(time.Now()),
						"type":        "pingbeat",
						"target_name": r.name,
						"target_addr": target,
						"tag":         r.tag,
						"rtt":         milliSeconds(r.rtt),
					}
					p.events.PublishEvent(event)
				}
				results[target] = nil
			}
		case <-fp.Done():
			if err = fp.Err(); err != nil {
				logp.Critical("Error: %Ping failed v", err)
			}
			break
		}
	}
	fp.Stop()
	return nil
}

func (p *Pingbeat) Cleanup(b *beat.Beat) error {
	return nil
}

func (p *Pingbeat) Stop() {
	close(p.done)
}
