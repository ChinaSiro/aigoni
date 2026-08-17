package config

import (
	"os"
	"sync"
	"time"
)

type Watcher struct {
	path    string
	current *Config
	modTime time.Time
	mu      sync.RWMutex
}

func NewWatcher(path string, cfg *Config) (*Watcher, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return &Watcher{path: path, current: cfg, modTime: info.ModTime()}, nil
}

func (w *Watcher) Config() *Config {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.current
}

func (w *Watcher) ReloadIfChanged() (*Config, bool, error) {
	info, err := os.Stat(w.path)
	if err != nil {
		return nil, false, err
	}
	if !info.ModTime().After(w.modTime) {
		return w.Config(), false, nil
	}
	cfg, err := Load(w.path)
	if err != nil {
		return nil, true, err
	}
	w.mu.Lock()
	w.current = cfg
	w.modTime = info.ModTime()
	w.mu.Unlock()
	return cfg, true, nil
}
