package main

import (
	"flag"
	"fmt"
	// "github.com/davecgh/go-spew/spew"
	"github.com/elastic/libbeat/cfgfile"
	"github.com/elastic/libbeat/common"
	"github.com/elastic/libbeat/logp"
	"github.com/elastic/libbeat/publisher"
	"github.com/elastic/libbeat/service"
	"github.com/tatsushid/go-fastping"
	"net"
	"os"
	"runtime"
	"time"
)

var Version = "0.0.1-alpha1"
var Name = "pingbeat"

type Pingbeat struct {
	isAlive  bool
	useIPv4  bool
	useIPv6  bool
	period   time.Duration
	pingType string
	targets  []Target
	events   chan common.MapStr
}

type Target struct {
	name      string
	tag       string
	ipv4Addrs []net.IPAddr
	ipv6Addrs []net.IPAddr
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
			t.ipv4Addrs = append(t.ipv4Addrs, net.IPAddr{IP: addrs[j]})
		} else {
			logp.Debug("pingbeat", "IPv6: %s\n", addrs[j].String())
			t.ipv6Addrs = append(t.ipv6Addrs, net.IPAddr{IP: addrs[j]})
		}
	}
}

func (p *Pingbeat) Run() error {

	p.isAlive = true

	fp := fastping.NewPinger()

	errInput, err := fp.Network(p.pingType)
	if err != nil {
		logp.Err("pingbeat", "Error: %v (input %v)", err, errInput)
	}

	details := make(map[string][2]string)

	for _, target := range p.targets {
		for i := 0; i < len(target.ipv4Addrs); i++ {
			fp.AddIP(target.ipv4Addrs[i].String())
			details[target.ipv4Addrs[i].String()] = [2]string{target.name, target.tag}
		}
		for i := 0; i < len(target.ipv6Addrs); i++ {
			fp.AddIP(target.ipv6Addrs[i].String())
			details[target.ipv6Addrs[i].String()] = [2]string{target.name, target.tag}
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
			fmt.Println(err)
		}
	}

	return nil
}

func (p *Pingbeat) Stop() {
	p.isAlive = false
}

func milliSeconds(d time.Duration) float64 {
	msec := d / time.Millisecond
	nsec := d % time.Millisecond
	return float64(msec) + float64(nsec)*1e-6
}

func main() {

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
	service.Cleanup()

}
