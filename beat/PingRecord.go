package pingbeat

import (
	"net"
	"time"
)

type PingRecord struct {
	Target net.Addr
	Sent   time.Time
}

func NewPingRecord(target net.Addr) *PingRecord {
	return &PingRecord{
		Target: target,
		Sent:   time.Now().UTC(),
	}
}
