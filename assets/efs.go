// based on https://www.alexedwards.net/blog/how-i-use-htmx-with-go
package assets

import (
	"embed"
	"io/fs"
)

//go:embed "html" "static" "xml"
var files embed.FS

var (
	HTMLFiles   = sub(files, "html")
	StaticFiles = sub(files, "static")
)

func sub(f embed.FS, dir string) fs.FS {
	sub, err := fs.Sub(f, dir)
	if err != nil {
		panic(err)
	}
	return sub
}
