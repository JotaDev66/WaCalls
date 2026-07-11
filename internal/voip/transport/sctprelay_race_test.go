package transport

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"wacalls/internal/voip/core"
)

func TestSctpRelayManagerRaces(t *testing.T) {
	m := NewSctpRelayManager(nil)
	conn := &relayConnection{
		id:     "1.1.1.1:3480",
		info:   RelayConfig{IP: "1.1.1.1", Port: 3480},
		stopCh: make(chan struct{}),
	}
	conn.setState(relayStateOpen)
	m.mu.Lock()
	m.connections[conn.id] = conn
	m.mu.Unlock()

	var wg sync.WaitGroup
	stop := make(chan struct{})
	run := func(n int, fn func(int)) {
		for i := range n {
			wg.Go(func() {
				for {
					select {
					case <-stop:
						return
					default:
					}
					fn(i)
				}
			})
		}
	}

	run(2, func(int) { m.BufferedAmount() })
	run(2, func(int) { m.ResendSubscriptions() })
	run(2, func(int) { m.HasConnection() })
	run(1, func(int) {
		if conn.getState() == relayStateOpen {
			conn.setState(relayStateFailed)
		} else {
			conn.setState(relayStateOpen)
		}
	})
	run(2, func(id int) { m.SetSsrc(uint32(id + 1)) })
	run(2, func(id int) { m.SetSubscriptionSsrc(uint32(id + 10)) })
	run(2, func(id int) { m.SetStreamSsrcs([]uint32{uint32(id)}, []uint32{uint32(id + 1)}) })
	run(2, func(int) { m.SetObserver(core.NopObserver{}) })
	run(2, func(id int) {
		cid := fmt.Sprintf("teardown-%d:3480", id)
		c := &relayConnection{id: cid, stopCh: make(chan struct{})}
		m.mu.Lock()
		m.connections[cid] = c
		m.mu.Unlock()
		var w sync.WaitGroup
		w.Add(2)
		go func() { defer w.Done(); m.failConnection(c) }()
		go func() { defer w.Done(); m.closeConnection(cid) }()
		w.Wait()
	})

	time.Sleep(300 * time.Millisecond)
	close(stop)
	wg.Wait()
}
