package pingbeat

import (
	"golang.org/x/net/icmp"
	"log"
	"net"
	"time"
)

type PingReply struct {
	text_payload   *icmp.Message
	binary_payload []byte
	ping_type      icmp.Type
	target         net.Addr
	rtt            time.Duration
}

func (reply *PingReply) Decode(n int) {
	rm, err := icmp.ParseMessage(reply.ping_type.Protocol(), reply.binary_payload[:n])
	if err != nil {
		log.Fatal(err)
	} else {
		reply.text_payload = rm
	}
}
