package util

import (
	"net"
	"sync"
)

// sharedLimitListener wraps a listener with a shared connection semaphore so
// multiple listeners (e.g. S3 primary + localhost) share one FD budget.
type sharedLimitListener struct {
	net.Listener
	sem chan struct{}
}

type sharedLimitConn struct {
	net.Conn
	sem  chan struct{}
	once sync.Once
}

func (c *sharedLimitConn) Close() error {
	var err error
	c.once.Do(func() {
		err = c.Conn.Close()
		<-c.sem
	})
	return err
}

func (l *sharedLimitListener) Accept() (net.Conn, error) {
	l.sem <- struct{}{}
	c, err := l.Listener.Accept()
	if err != nil {
		<-l.sem
		return nil, err
	}
	return &sharedLimitConn{Conn: c, sem: l.sem}, nil
}

// LimitListenersShareBudget wraps non-nil listeners so they share one maxConn
// connection budget. Returns a slice parallel to the input (nil stays nil).
func LimitListenersShareBudget(maxConn int, listeners ...net.Listener) []net.Listener {
	out := make([]net.Listener, len(listeners))
	copy(out, listeners)
	if maxConn <= 0 {
		return out
	}
	var active int
	for _, ln := range listeners {
		if ln != nil {
			active++
		}
	}
	if active == 0 {
		return out
	}
	sem := make(chan struct{}, maxConn)
	for i, ln := range listeners {
		if ln != nil {
			out[i] = &sharedLimitListener{Listener: ln, sem: sem}
		}
	}
	return out
}
