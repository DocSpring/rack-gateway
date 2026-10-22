package ratelimit

import (
	"net/netip"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// MaxClients is how many clients get a bucket of their own. Past that, new clients share one overflow bucket until
// sweeps make room, so memory stays bounded however many addresses send requests.
const MaxClients = 50_000

// sweepEvery is how often buckets are swept when there's room for more clients. A full map is swept at most once
// a second.
const sweepEvery = time.Minute

// buckets keeps a token bucket per client, for at most max clients.
type buckets struct {
	rate  rate.Limit
	burst int
	max   int
	now   func() time.Time

	mu        sync.Mutex
	clients   map[string]*rate.Limiter
	overflow  *rate.Limiter
	lastSweep time.Time
}

func newBuckets(perSecond rate.Limit, burst int) *buckets {
	return &buckets{
		rate:     perSecond,
		burst:    burst,
		max:      MaxClients,
		now:      time.Now,
		clients:  make(map[string]*rate.Limiter),
		overflow: rate.NewLimiter(perSecond, burst),
	}
}

// take reports whether the client at ip has a token, and takes it if so.
func (b *buckets) take(ip string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	b.maybeSweep(now)
	key := clientKey(ip)
	limiter := b.clients[key]
	if limiter == nil {
		if len(b.clients) >= b.max {
			return b.overflow.AllowN(now, 1)
		}
		limiter = rate.NewLimiter(b.rate, b.burst)
		b.clients[key] = limiter
	}
	return limiter.AllowN(now, 1)
}

// tokens is how many tokens the client at ip has left, without taking one or adding the client.
func (b *buckets) tokens(ip string) float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	limiter := b.clients[clientKey(ip)]
	if limiter == nil {
		if len(b.clients) < b.max {
			return float64(b.burst)
		}
		limiter = b.overflow
	}
	return limiter.TokensAt(b.now())
}

// maybeSweep sweeps every sweepEvery, or every second while the map is full. b.mu must be held.
func (b *buckets) maybeSweep(now time.Time) {
	since := now.Sub(b.lastSweep)
	if since < sweepEvery && (len(b.clients) < b.max || since < time.Second) {
		return
	}
	b.lastSweep = now
	b.sweep(now)
}

// sweep forgets the clients whose buckets have refilled. That changes nothing: a client without a bucket gets a
// full one. b.mu must be held.
func (b *buckets) sweep(now time.Time) {
	for key, limiter := range b.clients {
		if limiter.TokensAt(now) >= float64(b.burst) {
			delete(b.clients, key)
		}
	}
}

// clientKey groups an IPv6 address by its /64, the block one subscriber is usually given, so a client can't get
// a fresh bucket for each address it controls. IPv4 addresses (including IPv4-mapped IPv6) stand alone.
func clientKey(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
	addr = addr.Unmap()
	if addr.Is4() {
		return addr.String()
	}
	prefix, err := addr.WithZone("").Prefix(64)
	if err != nil {
		return ip
	}
	return prefix.String()
}
