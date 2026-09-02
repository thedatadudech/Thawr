package web

import (
	"embed"
	"io/fs"
)

//go:embed static
var static embed.FS

// Static returns the embedded admin UI files rooted at the static
// directory (index.html at the root).
func Static() (fs.FS, error) {
	sub, err := fs.Sub(static, "static")
	if err != nil {
		return nil, err
	}
	return sub, nil
}
