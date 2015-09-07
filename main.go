package main

import (
	"github.com/elastic/libbeat/beat"
)

var Version = "0.0.1-alpha2"
var Name = "pingbeat"

func main() {

	pingbeat := &Pingbeat{}

	b := beat.NewBeat(Name, Version, pingbeat)

	b.CommandLineSetup()

	b.LoadConfig()
	pingbeat.Config(b)

	b.Run()
}
