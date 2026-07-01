package static

import "embed"

const DistDir = "dist"

//go:embed all:dist
var Files embed.FS
