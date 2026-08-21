// Package web contains the viewer's embedded browser assets.
package web

import "embed"

// Files contains the HTML template and CSS needed by the viewer.
//
//go:embed page.html review.js styles.css
var Files embed.FS
