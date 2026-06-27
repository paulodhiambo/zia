package connector

import "sync"

type Registry struct {
	mu   sync.RWMutex
	conn map[string]Connector
}

func NewRegistry() *Registry {
	return &Registry{
		conn: make(map[string]Connector),
	}
}

func (r *Registry) Get(name string) (Connector, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.conn[name]
	return c, ok
}

func (r *Registry) All() []Connector {
	r.mu.RLock()
	defer r.mu.RUnlock()
	all := make([]Connector, 0, len(r.conn))
	for _, c := range r.conn {
		all = append(all, c)
	}
	return all
}

func (r *Registry) Register(name string, c Connector) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.conn[name] = c
}

func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.conn))
	for n := range r.conn {
		names = append(names, n)
	}
	return names
}
