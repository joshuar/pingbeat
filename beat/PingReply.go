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

func NewPingReply(n int, p string, b []byte, t icmp.Type) (*PingReply, error) {
	pr := &PingReply{}
	pr.target = p
	pr.ping_type = t
	pr.binary_payload = b
	rm, err := icmp.ParseMessage(pr.ping_type.Protocol(), pr.binary_payload[:n])
	if err != nil {
		return nil, err
	} else {
		pr.text_payload = rm
	}
	return pr, nil
}
