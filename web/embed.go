package web

import "embed"

//go:embed dist/*
var Assets embed.FS
