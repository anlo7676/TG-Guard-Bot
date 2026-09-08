package api

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

func (s *Server) clientAddress(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	peer = peer.Unmap()
	trusted := func(ip netip.Addr) bool {
		for _, cidr := range s.Config.TrustedProxies {
			if cidr.Contains(ip) {
				return true
			}
		}
		return false
	}
	if !trusted(peer) {
		return peer.String()
	}
	raw := r.Header.Get("X-Forwarded-For")
	if len(raw) > 2048 {
		return peer.String()
	}
	chain := strings.Split(raw, ",")
	if len(chain) > 32 {
		return peer.String()
	}
	current := peer
	for i := len(chain) - 1; i >= 0 && trusted(current); i-- {
		ip, e := netip.ParseAddr(strings.TrimSpace(chain[i]))
		if e != nil {
			return peer.String()
		}
		current = ip.Unmap()
	}
	return current.String()
}
