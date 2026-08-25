// Package web contains the viewer's embedded browser assets.
package web

import "embed"

// Files contains the HTML template, scripts, styles, and vendored browser
// dependencies needed by the viewer.
//
//go:embed templates/*.html generated/document-catalog.js generated/document-state.js generated/mermaid.js generated/review.js generated/review-fragments.js generated/review-highlights.js generated/review-htmx.js generated/review-navigation.js generated/review-panel.js generated/review-selection.js generated/styles.css generated/viewer.js generated/viewer-state.js vendor/htmx/htmx.min.js vendor/htmx/LICENSE vendor/mermaid/mermaid.tiny.js vendor/mermaid/LICENSE
var Files embed.FS
