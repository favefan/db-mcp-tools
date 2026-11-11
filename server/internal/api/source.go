package api

import (
	"net"
	"net/http"

	"github.com/realclientip/realclientip-go"
)

// SourceResolver applies only to MCP. Admin always uses the actual socket peer.
type SourceResolver func(*http.Request) string

func NewSourceResolver(trusted []string) (SourceResolver, error) {
	ranges, err := realclientip.AddressesAndRangesToIPNets(trusted...)
	if err != nil {
		return nil, err
	}
	strategy, err := realclientip.NewRightmostTrustedRangeStrategy("X-Forwarded-For", ranges)
	if err != nil {
		return nil, err
	}
	return func(r *http.Request) string {
		peer := source(r)
		ip := net.ParseIP(peer)
		for _, block := range ranges {
			if block.Contains(ip) {
				// Missing/invalid trusted-hop data must not regain the proxy's
				// often privileged loopback identity. Empty source is denied.
				return strategy.ClientIP(r.Header, r.RemoteAddr)
			}
		}
		return peer
	}, nil
}
