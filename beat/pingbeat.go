package pingbeat

import (
	"errors"
	// "github.com/davecgh/go-spew/spew"
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

type PingInfo struct {
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
	// Set up send/receive pools
	spool := pool.New()
	defer spool.Close()
	rpool := pool.New()
	defer rpool.Close()

	// Set up a ticker to loop for the period specified
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

	// Create IPv4/IPv6 connections where required
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

	// Create a new global state to track active ping requests
	state := NewPingState()

	for {
		select {
		case <-p.done:
			// We're done, stop the ticker and close send/receive pools
			ticker.Stop()
			spool.Close()
			rpool.Close()
			return nil
		case <-ticker.C:
			// Set up a batch queue job to send echo requests
			sendBatch := spool.Batch()
			// Send echo requests to batch queue
			if p.useIPv4 {
				go p.QueueRequests(state, c4, sendBatch)
			}
			if p.useIPv6 {
				go p.QueueRequests(state, c6, sendBatch)
			}

			for result := range sendBatch.Results() {
				// Grab info of the sent request
				info := result.Value().(*PingInfo)
				if err := result.Error(); err != nil {
					logp.Debug("pingbeat", "Send unsuccessful: %v", err)

				} else {
					var recv pool.WorkUnit
					// Queue a reciever for every request sent
					if net.ParseIP(info.Target).To4() != nil {
						recv = rpool.Queue(RecvPing(c4))
					} else {
						recv = rpool.Queue(RecvPing(c6))
					}

					recv.Wait()
					if err := recv.Error(); err != nil {
						logp.Debug("pingbeat", "Recv unsuccessful: %v", err)
					} else {
						// Grab info of the received reply
						ping := recv.Value().(*PingInfo)
						// If this reply doesn't indicate loss, record it
						if ping.Loss == false {
							target := ping.Target
							state.MU.Lock()
							rtt := time.Since(state.Pings[ping.Seq].Sent)
							delete(state.Pings, ping.Seq)
							state.MU.Unlock()
							go p.ProcessPing(target, rtt)
						} else {
							logp.Debug("pingbeat", "ICMP Error for Target %v: %v", ping.Target, ping.LossReason)
						}
					}
				}
			}
			// Clean out the global state of active ping requests marking any still
			// not received as lost
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
		// Record the queued ping in the global ping state
		state.MU.Lock()
		state.Pings[seq] = NewPingRecord(addr)
		state.MU.Unlock()

	}
	batch.QueueComplete()
}

func SendPing(conn *icmp.PacketConn, timeout time.Duration, seq int, addr string, network string) pool.WorkFunc {
	return func(wu pool.WorkUnit) (interface{}, error) {
		if wu.IsCancelled() {
			logp.Debug("pingbeat", "SendPing: workunit cancelled")
			return nil, nil
		}
		// Based on the connection, work out whether we are dealing with
		// IPv4 or IPv6 ICMP messages
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
		// Based on the network type, create the appropriate net.Addr type
		var dest net.Addr
		if network[:2] == "ip" {
			dest = &net.IPAddr{IP: net.ParseIP(addr)}
		} else {
			dest = &net.UDPAddr{IP: net.ParseIP(addr)}
		}
		// Create an ICMP Echo Request
		message := &icmp.Message{
			Type: ping_type, Code: 0,
			Body: &icmp.Echo{
				ID:   os.Getpid() & 0xffff,
				Seq:  seq,
				Data: []byte("pingbeat: y'know, for pings"),
			},
		}
		// Marshall the Echo request for sending via a connection
		binary, err := message.Marshal(nil)
		if err != nil {
			return nil, err
		}
		ping := &PingInfo{
			Seq:    seq,
			Target: addr,
		}
		// Send the request and if successful, set a read deadline for the connection
		if _, err := conn.WriteTo(binary, dest); err != nil {
			return ping, err
		} else {
			if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
				return ping, err
			}
			return ping, nil
		}
	}
}

func RecvPing(conn *icmp.PacketConn) pool.WorkFunc {
	return func(wu pool.WorkUnit) (interface{}, error) {
		if wu.IsCancelled() {
			logp.Debug("pingbeat", "RecvPing: workunit cancelled")
			return nil, nil
		}
		// Based on the connection, work out whether we are dealing with
		// IPv4 or IPv6 ICMP messages
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

		// Read data from the connection
		binary := make([]byte, 1500)
		n, peer, err := conn.ReadFrom(binary)
		if err != nil {
			return nil, err
		}
		// Parse the data into an ICMP message
		message, err := icmp.ParseMessage(ping_type.Protocol(), binary[:n])
		if err != nil {
			return nil, err
		}

		ping := &PingInfo{}
		// Switch for the ICMP message type
		switch message.Body.(type) {
		case *icmp.TimeExceeded:
			var d []byte
			d = message.Body.(*icmp.DstUnreach).Data
			header, _ := ipv4.ParseHeader(d[:len(d)-8])
			ping.Target = header.Dst.String()
			ping.Loss = true
			ping.LossReason = "Time Exceeded"
			return ping, nil
		case *icmp.PacketTooBig:
			var d []byte
			d = message.Body.(*icmp.DstUnreach).Data
			header, _ := ipv4.ParseHeader(d[:len(d)-8])
			ping.Target = header.Dst.String()
			ping.Loss = true
			ping.LossReason = "Packet Too Big"
			return ping, nil
		case *icmp.DstUnreach:
			var d []byte
			d = message.Body.(*icmp.DstUnreach).Data
			header, _ := ipv4.ParseHeader(d[:len(d)-8])
			ping.Target = header.Dst.String()
			ping.Loss = true
			ping.LossReason = "Destination Unreachable"
			return ping, nil
		case *icmp.Echo:
			ping.Seq = message.Body.(*icmp.Echo).Seq
			ping.Target = peer.String()
			ping.Loss = false
			return ping, nil
		default:
			err := errors.New("Unknown ICMP Packet")
			return nil, err
		}
	}
}

// milliSeconds converts seconds to milliseconds
func milliSeconds(d time.Duration) float64 {
	msec := d / time.Millisecond
	nsec := d % time.Millisecond
	return float64(msec) + float64(nsec)*1e-6
}
