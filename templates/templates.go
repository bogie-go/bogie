// Package templates holds the embedded layout that `bogie new` renders.
//
// Files ending in .tmpl are text/template with scaffold.Vars; everything else
// is copied as is. A segment beginning with dot_ becomes a dotfile on output,
// because go:embed skips real dotfiles.
package templates

import (
	"embed"
	"encoding/base64"
)

// App is the generated application, rooted at "app".
//
//go:embed app
var App embed.FS

// mascot is Bogie's mascot, a gopher and a ruby sharing a mine cart, sized
// for the welcome page. It travels inline as a data URI, the way Rails ships
// its own logo on its welcome page: no request leaves the page, and it works
// before the app has a static file in it.
//
//go:embed mascot.webp
var mascot []byte

// MascotDataURI is the mascot as an img src.
func MascotDataURI() string {
	return "data:image/webp;base64," + base64.StdEncoding.EncodeToString(mascot)
}
