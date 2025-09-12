package main

// getPluginDisplayName safely returns the display name for a plugin owner.
// Falls back to the owner key if no display name is set.
func getPluginDisplayName(owner string) string {
    pluginMu.RLock()
    name := pluginDisplayNames[owner]
    pluginMu.RUnlock()
    if name == "" {
        return owner
    }
    return name
}

