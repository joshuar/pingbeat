package main

import (
	"flag"
	"fmt"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"math/rand"
	"net"
	"os"
	"runtime"
	"time"

	"github.com/elastic/libbeat/cfgfile"
	"github.com/elastic/libbeat/common"
	"github.com/elastic/libbeat/logp"
	"github.com/elastic/libbeat/publisher"
	"github.com/elastic/libbeat/service"
)

var Version = "0.0.1-alpha1"
var Name = "pingbeat"

const (
	rawIPv4Ping       = "ip4:icmp"
	rawIPv6Ping       = "ip6:ipv6-icmp"
	icmpv4Proto       = 1
	icmpv6Proto       = 58
	icmpv4Length      = 512
	icmpv4EchoRequest = 8
	icmpv4EchoReply   = 0
	icmpv6EchoRequest = 128
	icmpv6EchoReply   = 129
)

type Pingbeat struct {
	isAlive bool
	useIPv4 bool
	useIPv6 bool

	period  time.Duration
	device  string
	targets []Target

	events chan common.MapStr
}

type Target struct {
	name      string
	tag       string
	ipv4Addrs []net.IP
	ipv6Addrs []net.IP
}

func (p *Pingbeat) Init(config PingConfig, events chan common.MapStr) error {

	if config.Period != nil {
		p.period = time.Duration(*config.Period) * time.Second
	} else {
		p.period = 1 * time.Second
	}
	logp.Debug("pingbeat", "Period %v\n", p.period)

	if config.Device != nil {
		p.device = *config.Device
	} else {
		p.device = "lo"
	}
	logp.Debug("pingbeat", "Device %s\n", p.device)

	if config.UseIPv4 != nil && *config.UseIPv4 == true {
		p.useIPv4 = true
	}
	if config.UseIPv6 != nil && *config.UseIPv6 == true {
		p.useIPv6 = true
	}

	// default to just useIPv4
	if config.UseIPv4 == nil && config.UseIPv6 == nil {
		p.useIPv4 = true
	}

	if config.Targets != nil {
		for tag, targets := range *config.Targets {
			logp.Debug("pingbeat", "Tag: %s\n", tag)
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

func (t *Target) Init(name string, tag string) {
	t.name = name
	t.tag = tag
	logp.Debug("pingbeat", "Getting IP addresses for %s:\n", t.name)
	addrs, err := net.LookupIP(t.name)
	if err != nil {
		logp.Err("pingbeat", "Failed to resolve %s to IP address\n", t.name)
	}
	for j := 0; j < len(addrs); j++ {
		if addrs[j].To4() != nil {
			logp.Debug("pingbeat", "IPv4: %s\n", addrs[j].String())
			t.ipv4Addrs = append(t.ipv4Addrs, addrs[j])
		} else {
			logp.Debug("pingbeat", "IPv6: %s\n", addrs[j].String())
			t.ipv6Addrs = append(t.ipv4Addrs, addrs[j])
		}
	}
}

func genICMPv4Msg() ([]byte, int, int) {
	var id = os.Getpid() & 0xffff
	var seq = rand.Intn(0xffff)

	msg := icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: icmpv4EchoRequest,
		Body: &icmp.Echo{
			ID: id, Seq: seq,
			Data: []byte("Pingbeat"),
		},
	}
	encodedMsg, err := msg.Marshal(nil)
	if err != nil {
		logp.Err("pingbeat", "%v", err)
	}

	return encodedMsg, id, seq
}

func privilegedIPv4Ping(target net.IP) {
	conn, connErr := icmp.ListenPacket(rawIPv4Ping, "0.0.0.0")
	if connErr != nil {
		logp.Err("pingbeat", "%v", connErr)
	}
	defer conn.Close()
	var icmpv4Msg []byte
	var icmpv4MsgId, icmpv4MsgSeq int
	icmpv4Msg, icmpv4MsgId, icmpv4MsgSeq = genICMPv4Msg()
	logp.Debug("pingbeat", "Sending ICMPv4 Echo-Request to %s:\n", target.String())
	if _, writeErr := conn.WriteTo(icmpv4Msg, &net.IPAddr{IP: target, Zone: "0.0.0.0"}); writeErr != nil {
		logp.Err("pingbeat", "%s", writeErr)
	}
	rb := make([]byte, icmpv4Length)
	_, _, readErr := conn.ReadFrom(rb)
	if readErr != nil {
		logp.Err("pingbeat", "%s", readErr)
	}
	rm, parseErr := icmp.ParseMessage(icmpv4Proto, rb)
	if parseErr != nil {
		logp.Err("pingbeat", "%s", parseErr)
	}
	if rm.Type == ipv4.ICMPTypeEchoReply {
		// TODO: add comparison of identifier and sequence number
		fmt.Printf("%v %v\n", icmpv4MsgId, icmpv4MsgSeq)
		fmt.Printf("got reply from from %+v", rm.Body)
	}
}

func (p *Pingbeat) doIPv4Pings() {
	for _, target := range p.targets {
		for i := 0; i < len(target.ipv4Addrs); i++ {
			go privilegedIPv4Ping(target.ipv4Addrs[i])
		}
	}
}

func (p *Pingbeat) Run() error {

	p.isAlive = true

	p.doIPv4Pings()
	var input string
	fmt.Scanln(&input)
	fmt.Println("done")

	return nil
}

func (t *Pingbeat) Stop() {

	t.isAlive = false
}

func main() {

	// over := make(chan bool)

	// Use our own FlagSet, because some libraries pollute the global one
	var cmdLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	cfgfile.CmdLineFlags(cmdLine, Name)
	logp.CmdLineFlags(cmdLine)
	service.CmdLineFlags(cmdLine)

	publishDisabled := cmdLine.Bool("N", false, "Disable actual publishing for testing")
	printVersion := cmdLine.Bool("version", false, "Print version and exit")

	cmdLine.Parse(os.Args[1:])

	if *printVersion {
		fmt.Printf("%s version %s (%s)\n", Name, Version, runtime.GOARCH)
		return
	}

	err := cfgfile.Read(&Config)
	// err := cfgfile.Read("./pingbeat.yml")

	logp.Init(Name, &Config.Logging)

	logp.Debug("main", "Initializing output plugins")
	if err = publisher.Publisher.Init(*publishDisabled, Config.Output,
		Config.Shipper); err != nil {

		logp.Critical(err.Error())
		os.Exit(1)
	}

	pingbeat := &Pingbeat{}

	if err = pingbeat.Init(Config.Input, publisher.Publisher.Queue); err != nil {
		logp.Critical(err.Error())
		os.Exit(1)
	}

	if cfgfile.IsTestConfig() {
		// all good, exit with 0
		os.Exit(0)
	}
	service.BeforeRun()

	service.HandleSignals(pingbeat.Stop)

	// Startup successful, disable stderr logging if requested by
	// cmdline flag
	logp.SetStderr()

	logp.Debug("main", "Pingbeat: you know, for pings")
	err = pingbeat.Run()
	if err != nil {
		logp.Critical("Sniffer main loop failed: %v", err)
		os.Exit(1)
	}

	logp.Debug("main", "Cleanup")

}
