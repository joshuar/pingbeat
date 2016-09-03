package pingbeat

import (
	"errors"
	"github.com/davecgh/go-spew/spew"
	"github.com/elastic/beats/libbeat/beat"
	"github.com/elastic/beats/libbeat/cfgfile"
	"github.com/elastic/beats/libbeat/common"
	"github.com/elastic/beats/libbeat/logp"
	"github.com/elastic/beats/libbeat/publisher"
	cfg "github.com/joshuar/pingbeat/config"
	"github.com/oschwald/geoip2-golang"
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
	ipv4network string
	ipv6network string
	ipv4targets map[string][2]string
	ipv6targets map[string][2]string
	geoipdb     *geoip2.Reader
	config      cfg.ConfigSettings
	events      publisher.Client
	done        chan struct{}
}

type PingSent struct {
	Seq    int
	Target net.Addr
	Sent   time.Time
}

type PingRecv struct {
	Seq        int
	Target     string
	Loss       bool
	LossReason string
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

	// Use period provided in config or default to 10s
	if p.config.Input.Period != nil {
		duration, err := time.ParseDuration(*p.config.Input.Period)
		p.period = duration
		if duration < time.Second || err != nil {
			logp.Warn("Config: Error parsing period or period too small: %v. Setting to default 10s", duration)
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

	// Check and load the GeoIP database
	if p.config.Input.GeoIPDB != nil {
		db, err := geoip2.Open(*p.config.Input.GeoIPDB)
		if err != nil {
			return err
		}
		p.geoipdb = db
		defer db.Close()
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
	spool := pool.New()
	defer spool.Close()
	rpool := pool.New()
	defer rpool.Close()

	ticker := time.NewTicker(p.period)
	defer ticker.Stop()

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

	state := NewPingState()

	for {
		select {
		case <-p.done:
			ticker.Stop()
			spool.Close()
			rpool.Close()
			return nil
		case <-ticker.C:
			sendBatch := spool.Batch()
			if p.useIPv4 {
				go p.QueueRequests(state, c4, sendBatch)
			}
			if p.useIPv6 {
				go p.QueueRequests(state, c6, sendBatch)
			}

			for result := range sendBatch.Results() {
				if err := result.Error(); err != nil {
					logp.Err("Send unsuccessful: %v", err)
				} else {
					// ping := result.Value().(icmp.Type)

					var recv pool.WorkUnit
					switch result.Value().(icmp.Type) {
					case ipv4.ICMPTypeEcho:
						recv = rpool.Queue(RecvPing(c4))
					case ipv6.ICMPTypeEchoRequest:
						recv = rpool.Queue(RecvPing(c6))
					default:
						logp.Err("Invalid ICMP message type")
					}
					recv.Wait()
					if err := recv.Error(); err != nil {
						logp.Err("Recv unsuccessful: %v", err)
					} else {
						ping := recv.Value().(*PingRecv)
						if ping.Loss == false {
							target := ping.Target
							state.MU.Lock()
							rtt := time.Since(state.Pings[ping.Seq].Sent)
							delete(state.Pings, ping.Seq)
							state.MU.Unlock()
							go p.ProcessPing(target, rtt)
						}
					}
				}
			}
			p.ProcessMissing(state)
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

func (p *Pingbeat) ProcessPing(target string, rtt time.Duration) {
	name, tag := p.FetchDetails(target)
	event := common.MapStr{
		"@timestamp":  common.Time(time.Now().UTC()),
		"type":        "pingbeat",
		"target_name": name,
		"target_addr": target,
		"tag":         tag,
		"rtt":         milliSeconds(rtt),
	}
	p.events.PublishEvent(event)
}

func (p *Pingbeat) ProcessMissing(state *PingState) {
	for seq_no, details := range state.Pings {
		name, tag := p.FetchDetails(details.Target)
		event := common.MapStr{
			"@timestamp":  common.Time(time.Now().UTC()),
			"type":        "pingbeat",
			"target_name": name,
			"target_addr": details.Target,
			"tag":         tag,
			"loss":        true,
		}
		p.events.PublishEvent(event)
		state.MU.Lock()
		delete(state.Pings, seq_no)
		state.MU.Unlock()
	}
}

func (p *Pingbeat) QueueRequests(state *PingState, conn *icmp.PacketConn, batch pool.Batch) {
	var network string
	targets := make(map[string][2]string)
	switch {
	case conn.IPv4PacketConn() != nil:
		targets = p.ipv4targets
		network = p.ipv4network
	case conn.IPv4PacketConn() != nil:
		targets = p.ipv6targets
		network = p.ipv6network
	default:
		logp.Err("QueueRequests: Unknown connection type")
	}
	for addr, _ := range targets {
		seq := state.GetSeqNo()
		batch.Queue(SendPing(conn, p.period, seq, addr, network))
		state.MU.Lock()
		state.Pings[seq] = NewPingRecord(addr)
		state.MU.Unlock()
	}
	batch.QueueComplete()
}

func SendPing(conn *icmp.PacketConn, timeout time.Duration, seq int, addr string, net string) pool.WorkFunc {
	return func(wu pool.WorkUnit) (interface{}, error) {
		if wu.IsCancelled() {
			logp.Debug("pingbeat", "SendPing: workunit cancelled")
			return nil, nil
		}
		var ping_type icmp.Type
		switch {
		case conn.IPv4PacketConn() != nil:
			ping_type = ipv4.ICMPTypeEcho
		case conn.IPv4PacketConn() != nil:
			ping_type = ipv6.ICMPTypeEchoRequest
		default:
			logp.Err("QueueRequests: Unknown connection type")
		}
		req, err := NewPingRequest(seq, ping_type, addr, net)
		if err != nil {
			logp.Err("QueueTargets: %v", err)
		}
		if _, err := conn.WriteTo(req.binary_payload, req.addr); err != nil {
			return nil, err
		} else {
			if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
				return nil, err
			}
			return req.ping_type, nil
		}
	}
}

func RecvPing(conn *icmp.PacketConn) pool.WorkFunc {
	return func(wu pool.WorkUnit) (interface{}, error) {
		if wu.IsCancelled() {
			logp.Debug("pingbeat", "RecvPing: workunit cancelled")
			return nil, nil
		}
		var ping_type icmp.Type
		switch {
		case conn.IPv4PacketConn() != nil:
			ping_type = ipv4.ICMPTypeEcho
		case conn.IPv4PacketConn() != nil:
			ping_type = ipv6.ICMPTypeEchoRequest
		default:
			err := errors.New("Unknown connection type")
			return nil, err
		}
		rep, err := NewPingReply(ping_type)
		if err != nil {
			return nil, err
		}
		n, peer, err := conn.ReadFrom(rep.binary_payload)
		if err != nil {
			return nil, err
		}
		rep.target = peer.String()
		rm, err := icmp.ParseMessage(rep.ping_type.Protocol(), rep.binary_payload[:n])
		if err != nil {
			return nil, err
		} else {
			rep.text_payload = rm
		}
		ping := &PingRecv{}
		switch rep.text_payload.Body.(type) {
		case *icmp.TimeExceeded:
			ping.Loss = true
			ping.LossReason = "Time Exceeded"
			return nil, err
		case *icmp.PacketTooBig:
			ping.LossReason = "Packet Too Big"
			ping.Loss = true
			return nil, err
		case *icmp.DstUnreach:
			ping.LossReason = "Destination Unreachable"
			var d []byte
			d = rep.text_payload.Body.(*icmp.DstUnreach).Data
			header, _ := ipv4.ParseHeader(d[:len(d)-8])
			spew.Dump(header)
			// rm, err := icmp.ParseMessage(ipv4.ICMPTypeEcho.Protocol(), d[len(d)-8:])
			// spew.Dump(rm)
			ping.Loss = true
			return nil, err
		case *icmp.Echo:
			ping.Seq = rep.text_payload.Body.(*icmp.Echo).Seq
			ping.Target = rep.target
			ping.Loss = false
			return ping, nil
		default:
			err := errors.New("Unknown ICMP Packet")
			return nil, err
		}
	}
}
