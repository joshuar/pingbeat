package pingbeat

import (
	"github.com/elastic/beats/libbeat/logp"
	"sync"
)

type PingState struct {
	MU    sync.RWMutex
	Pings map[int]*PingRecord
	SeqNo int
}

func NewPingState() *PingState {
	return &PingState{SeqNo: 0, Pings: make(map[int]*PingRecord)}
}

func (p *PingState) GetSeqNo() int {
	s := p.SeqNo
	p.SeqNo++
	// reset sequence no if we go above a 32-bit value
	if p.SeqNo > 65535 {
		logp.Debug("pingbeat", "Resetting sequence number")
		p.SeqNo = 0
	}
	return s
}
