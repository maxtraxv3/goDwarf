package main

import "sync"

type pluginConfigEntry struct {
	Name string
	Type string
}

var (
	pluginConfigMu      sync.RWMutex
	pluginConfigEntries = map[string][]pluginConfigEntry{}
)

func pluginAddConfig(owner, name, typ string) {
	if name == "" || typ == "" {
		return
	}
	pluginConfigMu.Lock()
	pluginConfigEntries[owner] = append(pluginConfigEntries[owner], pluginConfigEntry{Name: name, Type: typ})
	pluginConfigMu.Unlock()
	refreshPluginsWindow()
}

func pluginRemoveConfig(owner string) {
	pluginConfigMu.Lock()
	delete(pluginConfigEntries, owner)
	pluginConfigMu.Unlock()
	refreshPluginsWindow()
}
