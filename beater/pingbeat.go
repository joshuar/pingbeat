package beater

import (
	"errors"
	"fmt"
	// "github.com/davecgh/go-spew/spew"
	"github.com/elastic/beats/libbeat/beat"
	"github.com/elastic/beats/libbeat/common"
	"github.com/elastic/beats/libbeat/logp"
	"github.com/elastic/beats/libbeat/publisher"
	"github.com/joshuar/pingbeat/config"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
	"gopkg.in/go-playground/pool.v3"
	"net"
	"os"
	"time"
)

type Pingbeat struct {
	done        chan struct{}
	config      config.Config
	client      publisher.Client
	ipv4network string
	ipv6network string
	targets     map[string]Target
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

// Creates beater
func New(b *beat.Beat, cfg *common.Config) (beat.Beater, error) {
	config := config.DefaultConfig
	if err := cfg.Unpack(&config); err != nil {
		return nil, fmt.Errorf("Error reading config file: %v", err)
	}

	bt := &Pingbeat{
		done:   make(chan struct{}),
		config: config,
	}

	// Use privileged (i.e. raw socket) ping by default, else use a UDP ping
	if bt.config.Privileged {
		if os.Getuid() != 0 {
			return nil, fmt.Errorf("Privileged specified but not running with privileges!")
		}
		bt.ipv4network = "ip4:icmp"
		bt.ipv6network = "ip6:ipv6-icmp"
	} else {
		bt.ipv4network = "udp4"
		bt.ipv6network = "udp6"
	}

	// Fill the IPv4/IPv6 targets maps
	bt.targets = NewTargets(bt.config.Targets, bt.config.Privileged, bt.config.UseIPv4, bt.config.UseIPv6)
	return bt, nil
}

func (bt *Pingbeat) Run(b *beat.Beat) error {
	logp.Info("pingbeat is running! Hit CTRL-C to stop it.")

	bt.client = b.Publisher.Connect()

	// Set up send/receive pools
	spool := pool.NewLimited(100)
	defer spool.Close()

	// Set up a ticker to loop for the period specified
	ticker := time.NewTicker(bt.config.Period)
	defer ticker.Stop()
	timeout := time.NewTicker(bt.config.Timeout)
	defer timeout.Stop()

	// Create a new global state to track active ping requests
	state := NewPingState()

	// Start receivers to capture incoming ping replies
	// Create required connections
	var ipv4conn, ipv6conn *icmp.PacketConn
	var err error
	if bt.config.UseIPv4 {
		if ipv4conn, err = createConn(bt.ipv4network, "0.0.0.0"); err != nil {
			fmt.Errorf("Error creating %s connection: %v", bt.ipv4network, err)
			return nil
		}
		logp.Info("Using %s connection", bt.ipv4network)
		go RecvPings(bt, state, ipv4conn)
	}
	if bt.config.UseIPv6 {
		if ipv6conn, err = createConn(bt.ipv6network, "::"); err != nil {
			fmt.Errorf("Error creating %s connection: %v", bt.ipv6network, err)
			return nil
		}
		logp.Info("Using %s connection", bt.ipv6network)
		go RecvPings(bt, state, ipv6conn)
	}

	for {
		select {
		case <-bt.done:
			return nil
		case <-timeout.C:
			// Timeout reached, clean up any pending ping requests where there
			// has been no response
			go state.CleanPings(bt.config.Timeout)
		case <-ticker.C:
			// Batch queue echo request
			sendBatch := spool.Batch()
			go func(*icmp.PacketConn, *icmp.PacketConn) {
				for ip, target := range bt.targets {
					if net.ParseIP(ip).To4() != nil {
						logp.Debug("pingbeat", "Sending a ping")
						sendBatch.Queue(SendPing(ipv4conn, bt.config.Timeout, state.GetSeqNo(), target.Addr))
					} else {
						sendBatch.Queue(SendPing(ipv6conn, bt.config.Timeout, state.GetSeqNo(), target.Addr))
					}
				}
				sendBatch.QueueComplete()
			}(ipv4conn, ipv6conn)

			// For each successfully sent echo request
			for result := range sendBatch.Results() {
				// Grab info of the sent request
				if result.Value() == nil {
					logp.Debug("pingbeat", "Send unsuccessful: %v", result.Error())
					break
				}
				info := result.Value().(*PingInfo)
				if err := result.Error(); err != nil {
					logp.Debug("pingbeat", "Send unsuccessful: %v", err)
					bt.ProcessError(info.Target, "Send failed")
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

func (bt *Pingbeat) Stop() {
	bt.client.Close()
	close(bt.done)
}

func RecvPings(bt *Pingbeat, state *PingState, conn *icmp.PacketConn) {
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
		default:
			// err := errors.New("Unknown ICMP Packet")
		}
		if ping_id != id {
			logp.Debug("Ping response not from me: got %v, expected %v", string(ping_id), string(id))
		} else {
			ping.RTT = state.CalcPingRTT(ping.Seq, ping.Received)
			go bt.ProcessPing(ping)
		}
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

// FetchDetails takes a address as a string and returns the name and tag
// associated with that address in the Pingbeat struct
func (bt *Pingbeat) FetchDetails(t string) (string, string) {
	if _, found := bt.targets[t]; found {
		return bt.targets[t].Name, bt.targets[t].Tag
	} else {
		logp.Err("Error: %s not found in Pingbeat targets!", t)
		return "err", "err"
	}
}

func (bt *Pingbeat) ProcessPing(ping *PingInfo) {
	name, tag := bt.FetchDetails(ping.Target)
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
		logp.Debug("pingbeat", "Processed ping %v for %v (%v): %v", ping.Seq, name, ping.Target, ping.RTT)
		bt.client.PublishEvent(event)
	}
}

func (bt *Pingbeat) ProcessError(target string, error string) {
	name, tag := bt.FetchDetails(target)
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
		bt.client.PublishEvent(event)
	}
}

func createConn(n string, a string) (*icmp.PacketConn, error) {
	c, err := icmp.ListenPacket(n, a)
	if err != nil {
		return nil, err
	} else {
		return c, nil
	}
}

// milliSeconds converts seconds to milliseconds
func milliSeconds(d time.Duration) float64 {
	msec := d / time.Millisecond
	nsec := d % time.Millisecond
	return float64(msec) + float64(nsec)*1e-6
}
