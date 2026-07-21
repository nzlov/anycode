package static

import "embed"

const PWADir = "pwa"

//go:embed all:pwa
var Files embed.FS
