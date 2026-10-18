// Package web holds the dashboard the daemon serves on localhost (M5-06):
// a static page that reads the daemon's state from /api/state.
//
// tokens.css, lockup-light.svg and the fonts are copies of files in
// docs/brand, which go:embed cannot reach; TestBrandCopies keeps them equal.
package web

import "embed"

// Files is the dashboard: index.html, app.js, app.css and the brand files.
//
//go:embed index.html app.js app.css tokens.css lockup-light.svg fonts/*.woff2
var Files embed.FS
