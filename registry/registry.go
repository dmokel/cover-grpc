package registry

import (
	"sync"
	"time"
)

type serverItem struct {
	Addr      string
	startTime time.Time
}

// DRegistry ...
type DRegistry struct {
	timeout time.Duration
	mu      sync.Mutex
	servers map[string]*serverItem
}

const (
	defaultPath    = "_drpc_/registry"
	defaultTimeout = time.Minute * 5
)

// New ...
func New(timeout time.Duration) *DRegistry {
	return &DRegistry{
		timeout: timeout,
		servers: make(map[string]*serverItem),
	}
}
