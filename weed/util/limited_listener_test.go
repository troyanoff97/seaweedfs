package util

import (
	"net"
	"sync"
	"testing"
	"time"
)

func TestLimitListenersShareBudgetBlocksSecondAccept(t *testing.T) {
	a, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	wrapped := LimitListenersShareBudget(1, a, b)
	wa, wb := wrapped[0], wrapped[1]

	dialDone := make(chan struct{}, 2)
	go func() {
		c, err := net.Dial("tcp", a.Addr().String())
		if err == nil {
			defer c.Close()
			buf := make([]byte, 1)
			_, _ = c.Read(buf)
		}
		dialDone <- struct{}{}
	}()
	go func() {
		c, err := net.Dial("tcp", b.Addr().String())
		if err == nil {
			defer c.Close()
			buf := make([]byte, 1)
			_, _ = c.Read(buf)
		}
		dialDone <- struct{}{}
	}()

	c1, err := wa.Accept()
	if err != nil {
		t.Fatal(err)
	}

	second := make(chan struct{})
	go func() {
		c2, err := wb.Accept()
		if err == nil {
			_ = c2.Close()
		}
		close(second)
	}()

	select {
	case <-second:
		t.Fatal("second accept should block while first connection holds the shared slot")
	case <-time.After(100 * time.Millisecond):
	}

	_ = c1.Close()
	select {
	case <-second:
	case <-time.After(2 * time.Second):
		t.Fatal("second accept should proceed after first connection closes")
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { <-dialDone; wg.Done() }()
	go func() { <-dialDone; wg.Done() }()
	wg.Wait()
}
