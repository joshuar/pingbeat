package pingbeat

import (
	"time"
)

type PingRecord struct {
	Target string
	Sent   time.Time
}

func NewPingRecord(target string) *PingRecord {
	return &PingRecord{
		Target: target,
		Sent:   time.Now().UTC(),
	}
}
