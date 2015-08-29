package main

import (
	// "github.com/davecgh/go-spew/spew"
	"flag"
	"fmt"
	"github.com/elastic/libbeat/cfgfile"
	"github.com/elastic/libbeat/logp"
	"github.com/elastic/libbeat/publisher"
	"github.com/elastic/libbeat/service"
	"os"
	"runtime"
	"time"
)

var Version = "0.0.1-alpha1"
var Name = "pingbeat"

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
