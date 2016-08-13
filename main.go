package main

import (
	"github.com/elastic/beats/libbeat/beat"
	pingbeat "github.com/joshuar/pingbeat/beat"
	"os"
)

// You can overwrite these, e.g.: go build -ldflags "-X main.Version 1.0.0-beta3"
var Version = "0.1-alpha1"
var Name = "pingbeat"

func main() {
	if err := beat.Run(Name, Version, pingbeat.New()); err != nil {
		os.Exit(1)
	}
}
