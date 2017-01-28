package main

import (
	"os"

	"github.com/elastic/beats/libbeat/beat"

	"github.com/joshuar/pingbeat/beater"
)

func main() {
	err := beat.Run("pingbeat", "", beater.New)
	if err != nil {
		os.Exit(1)
	}
}
