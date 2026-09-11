package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

var pingInterval = 25 * time.Second

type event struct {
	name string
	data []byte
}

// hub fans events out to every connected event stream.
type hub struct {
	mu      sync.Mutex
	clients map[chan event]struct{}
	closed  bool
}

func newHub() *hub { return &hub{clients: map[chan event]struct{}{}} }

func (h *hub) subscribe() (<-chan event, func()) {
	ch := make(chan event, 64)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		close(ch)
		return ch, func() {}
	}
	h.clients[ch] = struct{}{}
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if _, ok := h.clients[ch]; ok {
			delete(h.clients, ch)
			close(ch)
		}
	}
}

func (h *hub) publish(name string, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- event{name: name, data: data}:
		default:
			// A client that cannot keep up is dropped; EventSource reconnects and refetches.
			delete(h.clients, ch)
			close(ch)
		}
	}
}

func (h *hub) close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for ch := range h.clients {
		delete(h.clients, ch)
		close(ch)
	}
}

func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming is not supported")
		return
	}
	ch, unsubscribe := s.events.subscribe()
	defer unsubscribe()

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, ": connected\n\n")
	fl.Flush()

	ping := time.NewTicker(pingInterval)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.name, ev.data)
			fl.Flush()
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		}
	}
}
