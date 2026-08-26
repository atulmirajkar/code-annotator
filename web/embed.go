// Package web contains the viewer's embedded browser assets.
package web

import "embed"

// Files contains the HTML template, scripts, styles, and vendored browser
// dependencies needed by the viewer.
//
//go:embed assets/*.svg templates/*.html generated/browser-storage.js generated/comparison-control.js generated/comparison-state.js generated/diff-divider.js generated/diff-overview.js generated/diff-overview-geometry.js generated/document-catalog.js generated/document-search.js generated/document-state.js generated/document-tree.js generated/layout-bootstrap.js generated/mermaid.js generated/review.js generated/review-fragments.js generated/review-highlights.js generated/review-htmx.js generated/review-navigation.js generated/review-panel.js generated/review-selection.js generated/styles.css generated/theme-bootstrap.js generated/theme-toggle.js generated/viewer.js generated/viewer-environment.js generated/viewer-layout.js generated/viewer-preferences.js generated/viewer-state.js vendor/htmx/htmx.min.js vendor/htmx/LICENSE vendor/mermaid/mermaid.tiny.js vendor/mermaid/LICENSE
var Files embed.FS
