package web

import "embed"

// Assets contains the built web UI files.
// Build with: cd web && npm run build
// The dist/ directory must exist for go:embed to work.
// If it doesn't exist, run: mkdir -p web/dist && touch web/dist/.gitkeep
//
//go:embed all:dist
var Assets embed.FS
