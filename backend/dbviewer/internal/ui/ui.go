// Package ui holds the web page of the viewer, embedded into the binary: one image, one command.
package ui

import "embed"

// FS contains index.html and its scripts and styles.
//
//go:embed index.html style.css app.js format.js
var FS embed.FS
