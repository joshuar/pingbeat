package pingbeat

import (
	// "errors"
	// "github.com/davecgh/go-spew/spew"
	"github.com/elastic/beats/libbeat/beat"
	"github.com/elastic/beats/libbeat/cfgfile"
	"github.com/elastic/beats/libbeat/common"
	"github.com/elastic/beats/libbeat/logp"
	"github.com/elastic/beats/libbeat/publisher"
	cfg "github.com/joshuar/pingbeat/config"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
	"gopkg.in/go-playground/pool.v3"
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
	iface       string
	ipv4targets map[string][2]string
	ipv6targets map[string][2]string
	config      cfg.ConfigSettings
	events      publisher.Client
	done        chan struct{}
}

func New() *Pingbeat {
	return &Pingbeat{}
}

// Config reads in the pingbeat configuration file, validating
// configuration parameters and setting default values where needed
func (p *Pingbeat) Config(b *beat.Beat) error {
	// Read in provided config file, bail if problem
	err := cfgfile.Read(&p.config, "")
	if err != nil {
		logp.Err("Error reading configuration file: %v", err)
		return err
	}

	// Use period provided in config or default to 5s
	if p.config.Input.Period != nil {
		duration, err := time.ParseDuration(*p.config.Input.Period)
		p.period = duration
		if duration < time.Second || err != nil {
			logp.Warn("pingbeat", "Error parsing duration or duration too small. Setting to default 10s")
			p.period = 10 * time.Second
		}
	} else {
		logp.Warn("pingbeat", "No period set. Setting to default 10s")
		p.period = 10 * time.Second
	}
	logp.Debug("pingbeat", "Period %v\n", p.period)

	// Use interface specified in config
	if &p.config.Input.Interface != nil {
		p.iface = *p.config.Input.Interface
	} else {
		logp.Critical("Error: no targets specified, cannot continue!")
		os.Exit(1)
	}

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

// Setup performs boilerplate Beats setup
func (p *Pingbeat) Setup(b *beat.Beat) error {
	p.events = b.Events
	p.done = make(chan struct{})
	return nil
}

func (p *Pingbeat) Run(b *beat.Beat) error {
	pool := pool.New()
	defer pool.Close()

	ticker := time.NewTicker(p.period)
	defer ticker.Stop()

	var seq_no = 0

	c, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		logp.Critical("Error: Could not create connection: %v", err)
	}
	defer c.Close()

	for {
		select {
		case <-p.done:
			pool.Cancel()
			return nil
		case <-ticker.C:
		}

		batch := pool.Batch()
		if p.useIPv4 {
			go func() {
				for addr, details := range p.ipv4targets {
					logp.Debug("pingbeat", "Adding target IP: %s, Name: %s, Tag: %s\n", addr, details[0], details[1])
					req := PingRequest{}
					req.Encode(seq_no, ipv4.ICMPTypeEcho, addr, p.iface)
					seq_no++
					batch.Queue(PingTarget(&req, c, p.period))
				}

				// DO NOT FORGET THIS OR GOROUTINES WILL DEADLOCK
				// if calling Cancel() it calles QueueComplete() internally
				batch.QueueComplete()
			}()
		}
		if p.useIPv6 {
			go func() {
				for addr, details := range p.ipv6targets {
					logp.Debug("pingbeat", "Adding target IP: %s, Name: %s, Tag: %s\n", addr, details[0], details[1])
					req := PingRequest{}
					req.Encode(seq_no, ipv6.ICMPTypeEchoRequest, addr, p.iface)
					seq_no++
					batch.Queue(PingTarget(&req, c, p.period))
				}

				// DO NOT FORGET THIS OR GOROUTINES WILL DEADLOCK
				// if calling Cancel() it calles QueueComplete() internally
				batch.QueueComplete()
			}()
		}
		for result := range batch.Results() {
			if err := result.Error(); err != nil {
				logp.Err("Error: %v: %v", err, result.Value().(PingReply).target)
				// handle error
				// maybe call batch.Cancel()
			} else {
				target := result.Value().(PingReply).target
				rtt := result.Value().(PingReply).rtt

				var name, tag string
				if _, found := p.ipv4targets[target]; found {
					name = p.ipv4targets[target][0]
					tag = p.ipv4targets[target][1]
				} else if _, found := p.ipv6targets[target]; found {
					name = p.ipv6targets[target][0]
					tag = p.ipv6targets[target][1]
				} else {
					logp.Warn("Error: Unexpected target returned: %s", target)
				}
				event := common.MapStr{
					"@timestamp":  common.Time(time.Now()),
					"type":        "pingbeat",
					"target_name": name,
					"target_addr": target,
					"tag":         tag,
					"rtt":         milliSeconds(rtt),
				}
				p.events.PublishEvent(event)
			}
		}
	}

}

func (p *Pingbeat) Cleanup(b *beat.Beat) error {
	return nil
}

func (p *Pingbeat) Stop() {
	close(p.done)
}

// AddTarget takes a target name and tag, fetches the IP addresses associated
// with it and adds them to the Pingbeat struct
func (p *Pingbeat) AddTarget(target string, tag string) {
	if addr := net.ParseIP(target); addr.String() == target {
		if addr.To4() != nil && p.useIPv4 {
			p.ipv4targets[addr.String()] = [2]string{target, tag}
		} else if p.useIPv6 {
			p.ipv6targets[addr.String()] = [2]string{target, tag}
		}
	} else {
		logp.Debug("pingbeat", "Getting IP addresses for %s:\n", target)
		ip4addr := make(chan string)
		ip6addr := make(chan string)
		go FetchIPs(ip4addr, ip6addr, target)
	lookup:
		for {
			select {
			case ip := <-ip4addr:
				if ip == "done" {
					break lookup
				} else if p.useIPv4 {
					logp.Debug("pingbeat", "IPv4: %s\n", ip)
					p.ipv4targets[ip] = [2]string{target, tag}
				}
			case ip := <-ip6addr:
				if ip == "done" {
					break lookup
				} else if p.useIPv6 {
					logp.Debug("pingbeat", "IPv6: %s\n", ip)
					p.ipv6targets[ip] = [2]string{target, tag}
				}
			}
		}
	}
}

// Addr2Name takes a net.IPAddr and returns the name and tag
// associated with that address in the Pingbeat struct
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

// milliSeconds converts seconds to milliseconds
func milliSeconds(d time.Duration) float64 {
	msec := d / time.Millisecond
	nsec := d % time.Millisecond
	return float64(msec) + float64(nsec)*1e-6
}

// FetchIPs takes a target hostname, resolves the IP addresses for that
// hostname via DNS and returns the results through the ip4addr/ip6addr
// channels
func FetchIPs(ip4addr, ip6addr chan string, target string) {
	addrs, err := net.LookupIP(target)
	if err != nil {
		logp.Warn("Failed to resolve %s to IP address, ignoring this target.\n", target)
	} else {
		for j := 0; j < len(addrs); j++ {
			if addrs[j].To4() != nil {
				ip4addr <- addrs[j].String()
			} else {
				ip6addr <- addrs[j].String()
			}
		}
	}
	ip4addr <- "done"
	close(ip4addr)
	ip6addr <- "done"
	close(ip6addr)
	return
}

func PingTarget(req *PingRequest, c *icmp.PacketConn, timeout time.Duration) pool.WorkFunc {
	return func(wu pool.WorkUnit) (interface{}, error) {
		rep := PingReply{}
		rep.binary_payload = make([]byte, 1500)
		rep.target = req.target.String()
		begin := time.Now()
		if _, err := c.WriteTo(req.binary_payload, req.target); err != nil {
			logp.Warn("pingbeat", "PingTarget: %v", err)
			return rep, err
		}
		if err := c.SetReadDeadline(time.Now().Add(timeout)); err != nil {
			logp.Warn("pingbeat", "PingTarget: %v", err)
			return rep, err
		}
		n, peer, err := c.ReadFrom(rep.binary_payload)
		if err != nil {
			logp.Warn("pingbeat", "PingTarget: %v", err)
			return rep, err
		}
		rep.rtt = time.Since(begin)
		rep.ping_type = req.ping_type
		rep.target = peer.String()
		rep.Decode(n)
		// if rep.text_payload.Body != req.text_payload.Body {
		// 	err := errors.New("sequence number mismatch")
		// 	return rep, err
		// }
		if wu.IsCancelled() {
			// return values not used
			return nil, nil
		}
		return rep, nil
	}
}
