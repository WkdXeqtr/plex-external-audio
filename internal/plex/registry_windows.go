//go:build windows

package plex

import "golang.org/x/sys/windows/registry"

// registryDataPath returns the data directory Plex has been moved to, if it has.
//
// Plex lets you relocate its data directory, and it records the new location
// only in the registry - nothing under the default path says it has moved. A
// tool that assumes %LOCALAPPDATA% therefore works on most machines and quietly
// finds nothing on the rest.
func registryDataPath() string {
	k, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Plex, Inc.\Plex Media Server`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, err := k.GetStringValue("LocalAppDataPath")
	if err != nil {
		return ""
	}
	return v
}

// InstallDir returns where Plex Media Server itself is installed.
//
// Both the 64-bit and the 32-bit view are checked, and the per-user key too:
// which one holds the value depends on how Plex was installed.
func InstallDir() string {
	for _, root := range []struct {
		key  registry.Key
		path string
	}{
		{registry.LOCAL_MACHINE, `SOFTWARE\Plex, Inc.\Plex Media Server`},
		{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Plex, Inc.\Plex Media Server`},
		{registry.CURRENT_USER, `Software\Plex, Inc.\Plex Media Server`},
	} {
		k, err := registry.OpenKey(root.key, root.path, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		v, _, err := k.GetStringValue("InstallFolder")
		k.Close()
		if err == nil && v != "" {
			return v
		}
	}
	return ""
}
