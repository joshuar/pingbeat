package pingbeat

import (
	"golang.org/x/net/icmp"
	"net"
	"os"
)

type PingRequest struct {
	text_payload   *icmp.Message
	binary_payload []byte
	ping_type      icmp.Type
	target         string
	addr           net.Addr
}

func NewPingRequest(seq_no int, ping_type icmp.Type, target string, network string) (*PingRequest, error) {
	pr := &PingRequest{}
	pr.target = target
	pr.addr = pr.toAddr(target, network)
	pr.ping_type = ping_type
	pr.text_payload = &icmp.Message{
		Type: pr.ping_type, Code: 0,
		Body: &icmp.Echo{
			ID:   os.Getpid() & 0xffff,
			Seq:  seq_no,
			Data: []byte("pingbeat: y'know, for pings"),
		},
	}
	binary, err := pr.text_payload.Marshal(nil)
	if err != nil {
		return nil, err
	} else {
		pr.binary_payload = binary
	}
	return pr, nil
}

func (pr *PingRequest) toAddr(t string, n string) net.Addr {
	if n[:2] == "ip" {
		return &net.IPAddr{IP: net.ParseIP(t)}
	} else {
		return &net.UDPAddr{IP: net.ParseIP(t)}
	}
}
