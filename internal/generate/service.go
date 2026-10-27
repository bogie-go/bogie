package generate

import "fmt"

// Service writes app/services/<name>/<name>.go and its test: a package with
// ctx-first functions that knows nothing about HTTP, which a controller, a
// job or a replay command all call the same way. It is not wired anywhere;
// whoever needs it declares the interface they want from it.
func Service(module, name string) ([]File, error) {
	if !namePattern.MatchString(name) {
		return nil, fmt.Errorf("%q is not a service name: lowercase letters, digits and underscores", name)
	}
	dir := "app/services/" + name + "/"
	return []File{
		{Path: dir + name + ".go", Content: serviceFile(name)},
		{Path: dir + name + "_suite_test.go", Content: suiteFile(name, Camel(name))},
		{Path: dir + name + "_test.go", Content: serviceTest(module, name)},
	}, nil
}

func serviceFile(name string) string {
	return fmt.Sprintf(`// Package %[1]s does one thing for the application. ctx first; it knows
// nothing about HTTP, so the controller, a job and the test call the same
// function with the same values.
package %[1]s

import (
	"context"
	"log/slog"
)

// store is the slice of persistence this package uses, declared HERE, next to
// its caller, rather than imported from models (rule 2). List only the
// methods this service calls; *adapter.Store satisfies it without ever
// mentioning this package, and the fake in the test stays a few lines:
//
//	type store interface {
//		GetPost(ctx context.Context, id string) (models.Post, error)
//	}
type store interface{}

// Service is %[1]s.
type Service struct {
	store store
	log   *slog.Logger
}

// New builds the service.
func New(store store, log *slog.Logger) *Service {
	return &Service{store: store, log: log}
}

// Run does the work. Rename it to say what that is.
func (s *Service) Run(ctx context.Context) error {
	s.log.InfoContext(ctx, "%[1]s: not implemented yet")
	return nil
}
`, name)
}

// suiteFile is the Ginkgo entry point of a package of its own: the one Test
// function go test finds, which runs every spec in the package's _test files.
func suiteFile(pkg, title string) string {
	return fmt.Sprintf(`package %[1]s_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func Test%[2]s(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, %[2]q)
}
`, pkg, title)
}

func serviceTest(module, name string) string {
	return fmt.Sprintf(`package %[1]s_test

import (
	"context"
	"io"
	"log/slog"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"%[2]s/app/services/%[1]s"
)

// fakeStore stands in for the store interface the service declares. Give it
// the methods that interface lists, backed by a map or a slice.
type fakeStore struct{}

var _ = Describe("Service", func() {
	var svc *%[1]s.Service

	Describe("Run", func() {
		Context("with an empty store", func() {
			BeforeEach(func() {
				svc = %[1]s.New(fakeStore{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			})

			It("succeeds", func() {
				Expect(svc.Run(context.Background())).To(Succeed())
			})
		})
	})
})
`, name, module)
}
