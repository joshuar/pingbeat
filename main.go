package main

import (
	"github.com/elastic/libbeat/beat"
	"github.com/joshuar/pingbeat/beater"
	"os"
)

// You can overwrite these, e.g.: go build -ldflags "-X main.Version 1.0.0-beta3"
var Version = "0.0.1-alpha2"
var Name = "pingbeat"

func main() {
	if err := beat.Run(Name, "", beater.New()); err != nil {
		os.Exit(1)
	}
}
