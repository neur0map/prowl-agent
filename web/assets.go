// Package web embeds the reproducibly built Prowl Workbench assets.
package web

import "embed"

// Assets contains the production workbench bundle. The generated dist tree is
// committed so normal Go builds do not require Node.js.
//
//go:embed dist
var Assets embed.FS
