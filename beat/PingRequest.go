package pingbeat

import (
	"golang.org/x/net/icmp"
	"log"
	"net"
	"os"
)

type PingRequest struct {
	text_payload   *icmp.Message
	binary_payload []byte
	ping_type      icmp.Type
	target         *net.IPAddr
	iface          string
}

func (request *PingRequest) Encode(seq_no int, ping_type icmp.Type, target string, iface string) {
	request.iface = iface
	request.target = &net.IPAddr{IP: net.ParseIP(target), Zone: iface}
	request.ping_type = ping_type
	request.text_payload = &icmp.Message{
		Type: request.ping_type, Code: 0,
		Body: &icmp.Echo{
			ID: os.Getpid() & 0xffff, Seq: seq_no,
			Data: []byte("HELLO-R-U-THERE"),
		},
	}
	binary, err := request.text_payload.Marshal(nil)
	if err != nil {
		log.Fatal(err)
	} else {
		request.binary_payload = binary
	}
}
