package main

import (
	"encoding/json"
	"fmt"
	"sync"
)

const clientBufferSize = 64

// SSEBroker manages fan-out to multiple SSE clients with backpressure
type SSEBroker struct {
	mu         sync.RWMutex
	clients    map[chan string]bool
}

// NewSSEBroker creates a new broker
func NewSSEBroker() *SSEBroker {
	return &SSEBroker{
		clients: make(map[chan string]bool),
	}
}

// Subscribe registers a new client and returns its channel
func (b *SSEBroker) Subscribe() chan string {
	ch := make(chan string, clientBufferSize)
	b.mu.Lock()
	b.clients[ch] = true
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes a client
func (b *SSEBroker) Unsubscribe(ch chan string) {
	b.mu.Lock()
	delete(b.clients, ch)
	b.mu.Unlock()
	close(ch)
}

// Broadcast sends an event to all clients with non-blocking sends (backpressure)
func (b *SSEBroker) Broadcast(event SSEEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	msg := fmt.Sprintf("event: %s\ndata: %s\n\n", event.Event, string(data))

	b.mu.RLock()
	defer b.mu.RUnlock()

	for ch := range b.clients {
		// Non-blocking send: drop if client buffer is full
		select {
		case ch <- msg:
		default:
			// Client is slow, skip this event
		}
	}
}

// ClientCount returns the number of connected SSE clients
func (b *SSEBroker) ClientCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.clients)
}
