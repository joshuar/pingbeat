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
	isAlive  bool
	useIPv4  bool
	useIPv6  bool
	period   time.Duration
	pingType string
	targets  []Target
	events   chan common.MapStr
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

	if config.Targets != nil {
		for tag, targets := range *config.Targets {
			for i := 0; i < len(targets); i++ {
				thisTarget := &Target{}
				thisTarget.Init(targets[i], tag)
				p.targets = append(p.targets, *thisTarget)
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

	details := make(map[string][2]string)

	for _, target := range p.targets {
		if p.useIPv4 {
			for i := 0; i < len(target.ipv4Addrs); i++ {
				fp.AddIP(target.ipv4Addrs[i].String())
				details[target.ipv4Addrs[i].String()] = [2]string{target.name, target.tag}
			}
		}
		if p.useIPv6 {
			for i := 0; i < len(target.ipv6Addrs); i++ {
				fp.AddIP(target.ipv6Addrs[i].String())
				details[target.ipv6Addrs[i].String()] = [2]string{target.name, target.tag}
			}
		}
	}
	fp.OnRecv = func(addr *net.IPAddr, rtt time.Duration) {
		name := details[addr.String()][0]
		tag := details[addr.String()][1]
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
