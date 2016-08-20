package pingbeat

import (
	"errors"
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
	ipv4network string
	ipv6network string
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
			logp.Warn("Config: Error parsing duration or duration too small: %v. Setting to default 10s", duration)
			p.period = 10 * time.Second
		}
	} else {
		logp.Warn("Config: No period set. Setting to default 10s")
		p.period = 10 * time.Second
	}
	logp.Debug("pingbeat", "Period %v\n", p.period)

	// Check if we can use privileged (i.e. raw socket) ping,
	// else use a UDP ping
	if *p.config.Input.Privileged {
		if os.Getuid() != 0 {
			err := errors.New("Privileged set but not running with privleges!")
			return err
		}
		p.ipv4network = "ip4:icmp"
		p.ipv6network = "ip6:ipv6-icmp"
	} else {
		p.ipv4network = "udp4"
		p.ipv6network = "udp6"

	}
	logp.Debug("pingbeat", "Using %v and/or %v for pings\n", p.ipv4network, p.ipv6network)

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
	logp.Debug("pingbeat", "Using IPv4: %v. Using IPv6: %v\n", p.useIPv4, p.useIPv6)

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
		err := errors.New("No targets specified, cannot continue!")
		return err
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

	createConn := func(n string, a string) *icmp.PacketConn {
		c, err := icmp.ListenPacket(n, a)
		if err != nil {
			logp.Err("Error creating connection: %v", err)
			return nil
		} else {
			return c
		}
	}

	c4 := &icmp.PacketConn{}
	if p.useIPv4 {
		c4 = createConn(p.ipv4network, "0.0.0.0")
	}
	defer c4.Close()

	c6 := &icmp.PacketConn{}
	if p.useIPv6 {
		c6 = createConn(p.ipv6network, "::")
	}
	defer c6.Close()

	for {
		batch := pool.Batch()
		select {
		case <-p.done:
			ticker.Stop()
			pool.Close()
			return nil
		case <-ticker.C:

			if p.useIPv4 {
				go func() {
					for addr, details := range p.ipv4targets {
						logp.Debug("pingbeat", "Adding target IP: %s, Name: %s, Tag: %s\n", addr, details[0], details[1])
						req := &PingRequest{}
						req.Encode(seq_no, ipv4.ICMPTypeEcho, addr)
						seq_no++
						// reset sequence no if we go above a 32-bit value
						if seq_no > 65535 {
							seq_no = 0
						}
						batch.Queue(PingTarget(req, c4, p.ipv4network, p.period))
					}
					batch.QueueComplete()
				}()
			}
			if p.useIPv6 {
				go func() {
					for addr, details := range p.ipv6targets {
						logp.Debug("pingbeat", "Adding target IP: %s, Name: %s, Tag: %s\n", addr, details[0], details[1])
						req := &PingRequest{}
						req.Encode(seq_no, ipv6.ICMPTypeEchoRequest, addr)
						seq_no++
						// reset sequence no if we go above a 32-bit value
						if seq_no > 65535 {
							seq_no = 0
						}
						batch.Queue(PingTarget(req, c6, p.ipv6network, p.period))
					}
					batch.QueueComplete()
				}()
			}
			for result := range batch.Results() {
				if err := result.Error(); err != nil {
					// If we get an error, treat it as loss
					// At some point we might try to distinguish
					// different errors
					logp.Err("Ping unsuccessful: %v", err)
					// result.Cancel()
					if result != nil {
						target := result.Value().(*PingReply).target
						name, tag := p.FetchDetails(target)
						event := common.MapStr{
							"@timestamp":  common.Time(time.Now()),
							"type":        "pingbeat",
							"target_name": name,
							"target_addr": target,
							"tag":         tag,
							"loss":        true,
						}
						p.events.PublishEvent(event)
					}
				} else {
					// Success, record the target and the rtt
					target := result.Value().(*PingReply).target
					rtt := result.Value().(*PingReply).rtt

					name, tag := p.FetchDetails(target)

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
					logp.Debug("pingbeat", "Target %s has an IPv4 address %s\n", target, ip)
					p.ipv4targets[ip] = [2]string{target, tag}
				}
			case ip := <-ip6addr:
				if ip == "done" {
					break lookup
				} else if p.useIPv6 {
					logp.Debug("pingbeat", "Target %s has an IPv6 address %s\n", target, ip)
					p.ipv6targets[ip] = [2]string{target, tag}
				}
			}
		}
	}
}

// Addr2Name takes a address as a string and returns the name and tag
// associated with that address in the Pingbeat struct
func (p *Pingbeat) FetchDetails(addr string) (string, string) {
	var name, tag string
	if _, found := p.ipv4targets[addr]; found {
		name = p.ipv4targets[addr][0]
		tag = p.ipv4targets[addr][1]
	} else if _, found := p.ipv6targets[addr]; found {
		name = p.ipv6targets[addr][0]
		tag = p.ipv6targets[addr][1]
	} else {
		logp.Err("Error: %s not found in Pingbeat targets!", addr)
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

// PingTarget is the workhorse function that sends a encoded ICMP packet to a target
// and decodes the response.  It takes in the packet, the connection and a timeout duration
// and will return the reply packet or an error
func PingTarget(req *PingRequest, c *icmp.PacketConn, network string, timeout time.Duration) pool.WorkFunc {
	return func(wu pool.WorkUnit) (interface{}, error) {
		rep := &PingReply{}
		rep.binary_payload = make([]byte, 1500)
		rep.ping_type = req.ping_type
		target2netAddr := func(t string, n string) net.Addr {
			if network[:2] == "ip" {
				return &net.IPAddr{IP: net.ParseIP(t)}
			} else {
				return &net.UDPAddr{IP: net.ParseIP(t)}
			}
		}
		target := target2netAddr(req.target, network)
		if err := c.SetReadDeadline(time.Now().Add(timeout)); err != nil {
			logp.Err("PingTarget: %v", err)
			return rep, err
		}
		begin := time.Now()
		if _, err := c.WriteTo(req.binary_payload, target); err != nil {
			logp.Err("PingTarget: %v", err)
			return rep, err
		}
		n, peer, err := c.ReadFrom(rep.binary_payload)
		if err != nil {
			logp.Err("PingTarget: %v", err)
			rep.target = req.target
			return rep, err
		}
		rep.target = peer.String()
		rep.rtt = time.Since(begin)
		rep.Decode(n)
		if wu.IsCancelled() {
			return nil, nil
		}
		return rep, nil
	}
}
