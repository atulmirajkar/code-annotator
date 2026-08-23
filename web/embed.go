// Package web contains the viewer's embedded browser assets.
package web

import "embed"

// Files contains the HTML template, scripts, styles, and vendored browser
// dependencies needed by the viewer.
//
//go:embed page.html mermaid.js review.js review-actions.js review-api.js review-dom.js review-highlights.js review-navigation.js review-panel.js review-render.js review-selection.js review-thread.js styles.css viewer.js vendor/mermaid/mermaid.tiny.js vendor/mermaid/LICENSE
var Files embed.FS
