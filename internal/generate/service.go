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
		{Path: dir + name + "_test.go", Content: serviceTest(name)},
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
// methods this service calls; *models.Store satisfies it without ever
// mentioning this package, and the fake in the test stays a few lines:
//
//	type store interface {
//		GetPost(ctx context.Context, id string) (domain.Post, error)
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

func serviceTest(name string) string {
	return fmt.Sprintf(`package %[1]s

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

type fakeStore struct{}

func TestRun(t *testing.T) {
	svc := New(fakeStore{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := svc.Run(context.Background()); err != nil {
		t.Fatalf("Run: %%v", err)
	}
}
`, name)
}
