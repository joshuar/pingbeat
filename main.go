package main

import (
	"github.com/elastic/libbeat/beat"
	pingbeat "github.com/joshuar/pingbeat/beat"
)

// You can overwrite these, e.g.: go build -ldflags "-X main.Version 1.0.0-beta3"
var Version = "0.0.1-alpha2"
var Name = "pingbeat"

func main() {
	beat.Run(Name, Version, pingbeat.New())
}
