//go:build !linux

package platform

import "fmt"

// PortalSaveFile is not supported on non-Linux platforms.
func PortalSaveFile(title, suggestedName, directory string) (string, error) {
	return "", fmt.Errorf("portal file chooser not supported on this platform")
}

// PortalSaveFiles is not supported on non-Linux platforms.
func PortalSaveFiles(title string, filenames []string, directory string) ([]string, error) {
	return nil, fmt.Errorf("portal file chooser not supported on this platform")
}
