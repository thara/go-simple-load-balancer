package main

import (
	"log"
	"net"
	"net/http/httputil"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
)

type Backend struct {
	URL          *url.URL
	Alive        bool
	mux          sync.RWMutex
	ReverseProxy *httputil.ReverseProxy
}

func (b *Backend) setAlive(alive bool) {
	b.mux.Lock()
	b.Alive = alive
	b.mux.Unlock()
}

func (b *Backend) isAlive() (alive bool) {
	b.mux.Lock()
	alive = b.Alive
	b.mux.Unlock()
	return
}

type ServerPool struct {
	backends []*Backend
	current  uint64
}

func (s *ServerPool) addBackend(b *Backend) {
	s.backends = append(s.backends, b)
}

func (s *ServerPool) nextIndex() int {
	return int(atomic.AddUint64(&s.current, uint64(1)) % uint64(len(s.backends)))
}

func (s *ServerPool) getNextPeer() *Backend {
	next := s.nextIndex()
	l := len(s.backends) + next
	for i := next; i < l; i++ {
		idx := i % len(s.backends)
		if s.backends[idx].isAlive() {
			if i != next {
				atomic.StoreUint64(&s.current, uint64(idx))
			}
			return s.backends[idx]
		}
	}
	return nil
}

func (s *ServerPool) HealthCheck() {
	for _, b := range s.backends {
		status := "up"
		alive := isBackendAlive(b.URL)
		b.setAlive(alive)
		if !alive {
			status = "down"
		}
		log.Printf("%s [%s]\n", b.URL, status)
	}
}

func (s *ServerPool) markBackendStatus(u *url.URL, alive bool) {
	for _, b := range s.backends {
		if b.URL.String() == u.String() {
			b.setAlive(alive)
			break
		}
	}
}

func isBackendAlive(u *url.URL) bool {
	timeout := 2 * time.Second
	conn, err := net.DialTimeout("tcp", u.Host, timeout)
	if err != nil {
		log.Println("Site unreachable, error: ", err)
		return false
	}
	_ = conn.Close()
	return true
}
