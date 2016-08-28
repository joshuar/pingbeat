package pingbeat

import (
	"golang.org/x/net/icmp"
)

type PingReply struct {
	text_payload   *icmp.Message
	binary_payload []byte
	ping_type      icmp.Type
	target         string
}

func NewPingReply(t icmp.Type) (*PingReply, error) {
	pr := &PingReply{}
	pr.ping_type = t
	pr.binary_payload = make([]byte, 1500)
	return pr, nil
}
