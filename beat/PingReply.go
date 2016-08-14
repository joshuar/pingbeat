package pingbeat

import (
	"github.com/elastic/beats/libbeat/logp"
	"golang.org/x/net/icmp"
	"time"
)

type PingReply struct {
	text_payload   *icmp.Message
	binary_payload []byte
	ping_type      icmp.Type
	target         string
	rtt            time.Duration
}

func (reply *PingReply) Decode(n int) {
	rm, err := icmp.ParseMessage(reply.ping_type.Protocol(), reply.binary_payload[:n])
	if err != nil {
		logp.Err("Error decoding packet: %v", err)
	} else {
		reply.text_payload = rm
	}
}
