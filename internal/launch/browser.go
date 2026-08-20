// Package launch opens the local viewer in the user's browser.
package launch

import "github.com/pkg/browser"

// OpenURL opens url in the user's default browser.
func OpenURL(url string) error {
	return browser.OpenURL(url)
}
