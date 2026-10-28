// Package public is a static file server with nowhere else to run: these
// files are embedded into the binary and tried as the second-to-last step
// of routing (app/controllers/routes.go), after every real route and before
// the development welcome page and the final 404. Plain files (robots.txt, a
// favicon, 404.html, 500.html) that sit below the router, not inside
// app/views.
package public

import "embed"

// Files is every file here, embedded so the running binary needs no
// filesystem beside it. Adding a new kind of file (an .ico, a .txt) means
// widening the go:embed pattern below to match.
//
//go:embed *.html images
var Files embed.FS

// NotFound and InternalServerError are read once, not per request, for the
// two places that answer with a fixed status rather than serving a file as
// itself: the final no-route handler, and the panic-recovery middleware.
var (
	NotFound            = mustRead("404.html")
	InternalServerError = mustRead("500.html")
)

func mustRead(name string) []byte {
	b, err := Files.ReadFile(name)
	if err != nil {
		panic(err)
	}
	return b
}
