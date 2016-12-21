package pingbeat

import (
	"github.com/elastic/beats/libbeat/logp"
	"gopkg.in/go-playground/pool.v3"
	"sync"
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

type PingState struct {
	MU    sync.RWMutex
	Pings map[int]*PingRecord
	SeqNo int
}

func NewPingState() *PingState {
	return &PingState{
		SeqNo: 0,
		Pings: make(map[int]*PingRecord),
	}
}

func (p *PingState) GetSeqNo() int {
	s := p.SeqNo
	p.SeqNo++
	// reset sequence no if we go above a 32-bit value
	if p.SeqNo > 65535 {
		logp.Debug("pingstate", "Resetting sequence number")
		p.SeqNo = 0
	}
	return s
}

func (p *PingState) AddPing(target string, seq int, sent time.Time) bool {
	p.MU.Lock()
	p.Pings[seq] = &PingRecord{
		Target: target,
		Sent:   sent,
	}
	p.MU.Unlock()
	return true
}

func (p *PingState) DelPing(seq int) pool.WorkFunc {
	return func(wu pool.WorkUnit) (interface{}, error) {
		if wu.IsCancelled() {
			// return values not used
			return nil, nil
		}
		p.MU.Lock()
		delete(p.Pings, seq)
		p.MU.Unlock()
		return seq, nil
	}
}

func (p *PingState) CalcPingRTT(seq int, received time.Time) time.Duration {
	p.MU.RLock()
	defer p.MU.RUnlock()
	if p.Pings[seq] != nil {
		return received.Sub(p.Pings[seq].Sent)
	} else {
		logp.Debug("pingstate", "Ping %v not found!", seq)
		return 0
	}
}

func (p *PingState) CleanPings(timeout time.Duration) {
	p.MU.Lock()
	defer p.MU.Unlock()
	for seq, details := range p.Pings {
		if p.Pings[seq].Sent.Add(timeout).Before(time.Now()) {
			logp.Debug("pingstate", "CleanPings: Removing Packet (Seq ID: %v) for %v", seq, details.Target)
			delete(p.Pings, seq)
		}
	}
}
