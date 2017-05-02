package beater

import (
	"errors"
	"net"

	"github.com/elastic/beats/libbeat/common"
	"github.com/elastic/beats/libbeat/logp"
	"gopkg.in/go-playground/pool.v3"
)

type Target struct {
	Addr net.Addr
	Name string
	Tags []string
	Desc string
}

type targetConfig struct {
	Name string   `config:"name"`
	Tags []string `config:"tags"`
	Desc string   `config:"desc"`
}

func NewTargets(cfg []*common.Config, privileged bool, ipv4 bool, ipv6 bool) map[string]Target {
	targets := make(map[string]Target)
	t := pool.New()
	defer t.Close()
	for _, c := range cfg {
		target := &targetConfig{}
		err := c.Unpack(target)
		if err != nil {
			logp.Critical("Error reading target config: %v", err)
		} else {
			work := t.Queue(AddTarget(target, privileged, ipv4, ipv6))
			work.Wait()
			if err := work.Error(); err != nil {
				logp.Err("Failed to add target %v: %v", work.Value().(*Target).Name, work.Error())
			} else {
				thisTarget := work.Value().(*Target)
				if thisTarget.Addr != nil {
					targets[thisTarget.Addr.String()] = *thisTarget
				}
			}
		}
	}
	return targets
}

// AddTarget takes a target name and tag, fetches the IP addresses associated
// with it and adds them to the Pingbeat struct
func AddTarget(target *targetConfig, privileged bool, ipv4 bool, ipv6 bool) pool.WorkFunc {
	return func(wu pool.WorkUnit) (interface{}, error) {
		if wu.IsCancelled() {
			// return values not used
			return nil, nil
		}
		t := &Target{
			Name: target.Name,
			Tags: target.Tags,
			Desc: target.Desc,
		}
		if net.ParseIP(t.Name) != nil {
			// Input is already an IP address, add it directly
			logp.Debug("pingbeat", "Adding target %s\n", t.Name)
			if privileged {
				t.Addr = &net.IPAddr{IP: net.ParseIP(t.Name)}
			} else {
				t.Addr = &net.UDPAddr{IP: net.ParseIP(t.Name)}
			}
		} else {
			// Input is a hostname, look up IP addrs and add
			addrs, err := net.LookupIP(t.Name)
			if err != nil {
				err := errors.New(t.Name)
				return t, err
			}
			for j := 0; j < len(addrs); j++ {
				// If we have an IPv4 address and we aren't using IPv4, ignore
				if addrs[j].To4() != nil && !ipv4 {
					logp.Debug("pingbeat", "Ignoring IPv4 address %s for target %s as not using IPv4\n", addrs[j].String(), t.Name)
					break
				}
				// If we have an IPv6 address and we aren't using IPv6, ignore
				if addrs[j].To4() == nil && !ipv6 {
					logp.Debug("pingbeat", "Ignoring IPv6 address %s for target %s as not using IPv6\n", addrs[j].String(), t.Name)
					break
				}
				// If we get a loopback address, ignore it
				if addrs[j].IsLoopback() {
					logp.Warn("Target %s resolves to a loopback address? Not adding as target.\n", t.Name)
					break
				}
				addrString := addrs[j].String()
				logp.Debug("pingbeat", "Target %s has an address %s\n", t.Name, addrString)
				if privileged {
					t.Addr = &net.IPAddr{IP: net.ParseIP(addrString)}
				} else {
					t.Addr = &net.UDPAddr{IP: net.ParseIP(addrString)}
				}
			}
		}
		return t, nil
	}
}
