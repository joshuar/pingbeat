package pingbeat

import (
	"errors"
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

func (p *PingState) CalcPingRTT(seq int) pool.WorkFunc {
	return func(wu pool.WorkUnit) (interface{}, error) {
		if wu.IsCancelled() {
			// return values not used
			return nil, nil
		}
		p.MU.RLock()
		defer p.MU.RUnlock()
		if p.Pings[seq] != nil {
			return time.Since(p.Pings[seq].Sent), nil
		} else {
			return nil, errors.New("Got a ping response that we aren't tracking")
		}
	}
}

func (p *PingState) AddPing(target string) pool.WorkFunc {
	return func(wu pool.WorkUnit) (interface{}, error) {
		if wu.IsCancelled() {
			// return values not used
			return nil, nil
		}
		seq := p.GetSeqNo()
		p.MU.Lock()
		p.Pings[seq] = &PingRecord{
			Target: target,
			Sent:   time.Now().UTC(),
		}
		p.MU.Unlock()
		return seq, nil
	}
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

func (p *PingState) CleanPings(timeout time.Duration) pool.WorkFunc {
	return func(wu pool.WorkUnit) (interface{}, error) {
		if wu.IsCancelled() {
			// return values not used
			return nil, nil
		}
		p.MU.Lock()
		for seq, details := range p.Pings {
			if p.Pings[seq].Sent.Add(timeout).Before(time.Now()) {
				logp.Debug("pingbeat", "CleanStalePings: Removing Packet (Seq ID: %v) for %v", seq, details)
				delete(p.Pings, seq)
			}
		}
		p.MU.Unlock()
		return nil, nil
	}
}
