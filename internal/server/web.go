package server

import (
	"embed"
	"io/fs"
)

//go:embed web/*
var rawWebAssets embed.FS

// webAssets is the filesystem containing the static web files, stripped of the "web" prefix
var webAssets fs.FS

func init() {
	var err error
	webAssets, err = fs.Sub(rawWebAssets, "web")
	if err != nil {
		panic(err)
	}
}
