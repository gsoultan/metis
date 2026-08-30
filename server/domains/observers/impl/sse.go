package impl

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/gsoultan/metis/server/domains/entities"
)

type SSEObserver struct {
	mu      sync.RWMutex
	clients map[chan string]bool
}

func NewSSEObserver() *SSEObserver {
	return &SSEObserver{
		clients: make(map[chan string]bool),
	}
}

func (o *SSEObserver) OnEvent(ctx context.Context, event entities.ProcessEvent) {
	o.Broadcast(event)
}

func (o *SSEObserver) Broadcast(data any) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return
	}

	o.mu.RLock()
	defer o.mu.RUnlock()

	msg := fmt.Sprintf("data: %s\n\n", string(jsonData))
	for clientChan := range o.clients {
		select {
		case clientChan <- msg:
		default:
			// Client slow or disconnected
		}
	}
}

func (o *SSEObserver) AddClient() chan string {
	o.mu.Lock()
	defer o.mu.Unlock()
	ch := make(chan string, 10)
	o.clients[ch] = true
	return ch
}

func (o *SSEObserver) RemoveClient(ch chan string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.clients, ch)
	close(ch)
}
