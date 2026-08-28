//go:build !windows

package plex

// registryDataPath has nothing to answer outside Windows: everywhere else the
// data directory is either the default or given on the command line.
func registryDataPath() string { return "" }

// InstallDir is likewise Windows-only. On other platforms the server binaries
// live in a distribution-controlled location and the caller is expected to know
// it or be told.
func InstallDir() string { return "" }
