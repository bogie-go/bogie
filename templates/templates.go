// Package templates holds the embedded layout that `bogie new` renders.
//
// Files ending in .tmpl are text/template with scaffold.Vars; everything else
// is copied as is. A segment beginning with dot_ becomes a dotfile on output,
// because go:embed skips real dotfiles.
package templates

import "embed"

// App is the generated application, rooted at "app".
//
//go:embed app
var App embed.FS
