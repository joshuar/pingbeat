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
	timeout     time.Duration
	ipv4network string
	ipv6network string
	ipv4conn    *icmp.PacketConn
	ipv6conn    *icmp.PacketConn
	targets     map[string]Target
	geoipdb     *geoip2.Reader
	config      cfg.ConfigSettings
	events      publisher.Client
	done        chan struct{}
}

type Target struct {
	Addr net.Addr
	Name string
	Tag  string
}

type PingInfo struct {
	Seq        int
	Target     string
	Sent       time.Time
	Received   time.Time
	RTT        time.Duration
	Loss       bool
	LossReason string
}

func New() *Pingbeat {
	return &Pingbeat{}
}

// Config reads in the pingbeat configuration file, validating
// configuration parameters and setting default values where needed
func (p *Pingbeat) Config(b *beat.Beat) error {
	createConn := func(n string, a string) (*icmp.PacketConn, error) {
		logp.Info("%v, %v", n, a)
		c, err := icmp.ListenPacket(n, a)
		if err != nil {
			return nil, err
		} else {
			return c, nil
		}
	}

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

	p.timeout = 10 * p.period
	logp.Debug("pingbeat", "Timeout %v\n", p.timeout)

	// Check if we can use privileged (i.e. raw socket) ping,
	// else use a UDP ping
	if *p.config.Input.Privileged {
		if os.Getuid() != 0 {
			err := errors.New("Privileged specified but not running with privleges!")
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
	if &p.config.Input.UseIPv4 == nil && &p.config.Input.UseIPv6 == nil {
		err := errors.New("Neither useIPv4 or useIPv6 specified.  At least one required!")
		return err
	}
	p.useIPv4 = *p.config.Input.UseIPv4
	if p.useIPv4 {
		p.ipv4conn, err = createConn(p.ipv4network, "0.0.0.0")
		if err != nil {
			return err
		}
	}
	p.useIPv6 = *p.config.Input.UseIPv6
	if p.useIPv6 {
		p.ipv6conn, err = createConn(p.ipv6network, "::")
		if err != nil {
			return err
		}
	}
	logp.Debug("pingbeat", "Using IPv4: %v. Using IPv6: %v\n", p.useIPv4, p.useIPv6)

	// Fill the IPv4/IPv6 targets maps
	p.targets = make(map[string]Target)
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
	spool := pool.NewLimited(100)
	defer spool.Close()

	// Set up a ticker to loop for the period specified
	ticker := time.NewTicker(p.period)
	defer ticker.Stop()
	timeout := time.NewTicker(p.timeout)
	defer timeout.Stop()

	// Create a new global state to track active ping requests
	state := NewPingState()

	// Start receivers to capture incoming ping replies
	go RecvPings(p, state, p.ipv4conn)
	go RecvPings(p, state, p.ipv6conn)

	for {
		select {
		case <-p.done:
			// We're done, stop the ticker and close send/receive pools
			p.ipv4conn.Close()
			p.ipv6conn.Close()
			ticker.Stop()
			timeout.Stop()
			spool.Close()
			return nil
		case <-timeout.C:
			// Timeout reached, clean up any pending ping requests where there
			// has been no response
			go state.CleanPings(p.timeout)
		case <-ticker.C:
			// Batch queue echo request
			sendBatch := spool.Batch()
			go func() {
				for ip, target := range p.targets {
					if net.ParseIP(ip).To4() != nil {
						sendBatch.Queue(SendPing(p.ipv4conn, p.timeout, state.GetSeqNo(), target.Addr))
					} else {
						sendBatch.Queue(SendPing(p.ipv6conn, p.timeout, state.GetSeqNo(), target.Addr))
					}
				}
				sendBatch.QueueComplete()
			}()

			// For each successfully sent echo request
			for result := range sendBatch.Results() {
				// Grab info of the sent request
				info := result.Value().(*PingInfo)
				if err := result.Error(); err != nil {
					logp.Debug("pingbeat", "Send unsuccessful: %v", err)
					p.ProcessError(info.Target, "Send failed")
				} else {
					success := state.AddPing(info.Target, info.Seq, info.Sent)
					if !success {
						logp.Err("Error adding ping (%v:%v) to state", info.Seq, info.Target)
					}
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
	string2addr := func(t string) net.Addr {
		switch p.ipv4network[:2] {
		case "ip":
			return &net.IPAddr{IP: net.ParseIP(t)}
		default:
			return &net.UDPAddr{IP: net.ParseIP(t)}
		}
	}

	if net.ParseIP(target) != nil {
		p.targets[target] = Target{
			Addr: string2addr(target),
			Name: target,
			Tag:  tag,
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
					p.targets[ip] = Target{
						Addr: string2addr(ip),
						Name: target,
						Tag:  tag,
					}
					logp.Debug("pingbeat", "Target %s has an IPv4 address %s\n", target, ip)
				}
			case ip := <-ip6addr:
				if ip == "done" {
					break lookup
				} else if p.useIPv6 {
					p.targets[ip] = Target{
						Addr: string2addr(ip),
						Name: target,
						Tag:  tag,
					}
					logp.Debug("pingbeat", "Target %s has an IPv6 address %s\n", target, ip)
				}
			}
		}
	}
}

// Addr2Name takes a address as a string and returns the name and tag
// associated with that address in the Pingbeat struct
func (p *Pingbeat) FetchDetails(t string) (string, string) {
	if _, found := p.targets[t]; found {
		return p.targets[t].Name, p.targets[t].Tag
	} else {
		logp.Err("Error: %s not found in Pingbeat targets!", t)
		return "err", "err"
	}
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

func (p *Pingbeat) ProcessPing(ping *PingInfo) {
	name, tag := p.FetchDetails(ping.Target)
	if name == "err" {
		logp.Err("No details for %v in targets!", ping.Target)
	} else {
		event := common.MapStr{
			"@timestamp":  common.Time(time.Now().UTC()),
			"type":        "pingbeat",
			"target_name": name,
			"target_addr": ping.Target,
			"tag":         tag,
			"rtt":         milliSeconds(ping.RTT),
		}
		p.events.PublishEvent(event)
	}
}

func (p *Pingbeat) ProcessError(target string, error string) {
	name, tag := p.FetchDetails(target)
	if name == "err" {
		logp.Err("No details for %v in targets!", target)
	} else {
		event := common.MapStr{
			"@timestamp":  common.Time(time.Now().UTC()),
			"type":        "pingbeat",
			"target_name": name,
			"target_addr": target,
			"tag":         tag,
			"loss":        true,
			"reason":      error,
		}
		p.events.PublishEvent(event)
	}
}

func SendPing(conn *icmp.PacketConn, timeout time.Duration, seq int, addr net.Addr) pool.WorkFunc {
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

		// Create an ICMP Echo Request
		var id = os.Getpid() & 0xffff
		message := &icmp.Message{
			Type: ping_type, Code: 0,
			Body: &icmp.Echo{
				ID:   id,
				Seq:  seq,
				Data: []byte("pingbeat: y'know, for pings"),
			},
		}
		logp.Debug("pingbeat", "Echo request %v: %v", seq, addr.String())
		// Marshall the Echo request for sending via a connection
		binary, err := message.Marshal(nil)
		if err != nil {
			return nil, err
		}
		var t string
		switch addr.(type) {
		case *net.UDPAddr:
			t, _, _ = net.SplitHostPort(addr.String())
		case *net.IPAddr:
			t = addr.String()
		default:
			err := errors.New("Unknown address type")
			return nil, err
		}

		ping := &PingInfo{
			Seq:    seq,
			Target: t,
		}
		// Send the request and if successful, set a read deadline for the connection
		if _, err := conn.WriteTo(binary, addr); err != nil {
			return ping, err
		} else {
			if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
				return ping, err
			}
			ping.Sent = time.Now().UTC()
			return ping, nil
		}
	}
}

func RecvPings(p *Pingbeat, state *PingState, conn *icmp.PacketConn) {
	for {
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
			logp.Err("Error parsing connection: %v", err)
			break
		}

		var id = os.Getpid() & 0xffff

		// Read data from the connection
		binary := make([]byte, 1500)
		n, peer, err := conn.ReadFrom(binary)
		if err != nil {
			binary = nil
			logp.Err("Couldn't read from connection: %v", err)
			break
		}
		var target string
		switch peer.(type) {
		case *net.UDPAddr:
			target, _, _ = net.SplitHostPort(peer.String())
		case *net.IPAddr:
			target = peer.String()
		default:
			logp.Err("Error parsing received address %v", target)
			break
		}

		if n == 0 {
			break
		}
		// Parse the data into an ICMP message
		message, err := icmp.ParseMessage(ping_type.Protocol(), binary[:n])
		if err != nil {
			logp.Err("Couldn't parse response: %v", err)
			break
		}

		ping := &PingInfo{}
		var ping_id int
		// Switch for the ICMP message type
		switch message.Body.(type) {
		case *icmp.TimeExceeded:
			d := message.Body.(*icmp.TimeExceeded).Data
			header, _ := ipv4.ParseHeader(d[:len(d)-8])
			ping.Target = header.Dst.String()
			ping.Loss = true
			ping.LossReason = "Time Exceeded"
			logp.Debug("pingbeat", "Time exceeded %v", ping.Target)
		case *icmp.PacketTooBig:
			d := message.Body.(*icmp.PacketTooBig).Data
			header, _ := ipv4.ParseHeader(d[:len(d)-8])
			ping.Target = header.Dst.String()
			ping.Loss = true
			ping.LossReason = "Packet Too Big"
			logp.Debug("pingbeat", "Packet too big %v", ping.Target)
		case *icmp.DstUnreach:
			d := message.Body.(*icmp.DstUnreach).Data
			header, _ := ipv4.ParseHeader(d[:len(d)-8])
			ping.Target = header.Dst.String()
			ping.Loss = true
			ping.LossReason = "Destination Unreachable"
			logp.Debug("pingbeat", "Destination unreachable %v", ping.Target)
		case *icmp.Echo:
			ping.Seq = message.Body.(*icmp.Echo).Seq
			ping_id = message.Body.(*icmp.Echo).ID
			ping.Target = target
			ping.Loss = false
			ping.Received = time.Now().UTC()
			logp.Debug("pingbeat", "Echo Reply %v: %v", ping.Seq, ping.Target)
		default:
			// err := errors.New("Unknown ICMP Packet")
		}
		if ping_id != id {
			logp.Debug("Ping response not from me: got %v, expected %v", string(ping_id), string(id))
		} else {
			ping.RTT = state.CalcPingRTT(ping.Seq, ping.Received)
			go p.ProcessPing(ping)
		}
	}
}

// milliSeconds converts seconds to milliseconds
func milliSeconds(d time.Duration) float64 {
	msec := d / time.Millisecond
	nsec := d % time.Millisecond
	return float64(msec) + float64(nsec)*1e-6
}
