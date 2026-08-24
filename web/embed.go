// Package web contains the viewer's embedded browser assets.
package web

import "embed"

// Files contains the HTML template, scripts, styles, and vendored browser
// dependencies needed by the viewer.
//
//go:embed page.html generated/document-tree.js generated/mermaid.js generated/review.js generated/review-actions.js generated/review-api.js generated/review-dom.js generated/review-highlights.js generated/review-navigation.js generated/review-panel.js generated/review-render.js generated/review-selection.js generated/review-thread.js generated/styles.css generated/viewer.js vendor/htmx/htmx.min.js vendor/htmx/LICENSE vendor/mermaid/mermaid.tiny.js vendor/mermaid/LICENSE
var Files embed.FS
