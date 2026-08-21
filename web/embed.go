// Package web contains the viewer's embedded browser assets.
package web

import "embed"

// Files contains the HTML template, scripts, styles, and vendored browser
// dependencies needed by the viewer.
//
//go:embed page.html mermaid.js review.js styles.css vendor/mermaid/mermaid.tiny.js vendor/mermaid/LICENSE
var Files embed.FS
