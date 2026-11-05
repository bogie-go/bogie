package generate

import (
	"fmt"
	"go/format"
	"strings"
)

// ownerID is the user id a policied factory stamps on every record, and the
// subject the generated request specs sign in as. It is a string because the
// token's subject is a string: the user lives wherever the token was minted.
const ownerID = "user-1"

// otherID is a second subject, used wherever a spec needs someone who does
// not own the record.
const otherID = "user-2"

// PolicyRoles are the role strings the generated spec puts through every
// action. A role is only a string on the actor; nothing here looks one up.
var PolicyRoles = []string{"member", "admin"}

// EnsureOwner makes user_id the owner column, adding it when the attributes
// do not already name it. The policy compares it to the actor id, so it has
// to be a string.
func EnsureOwner(attrs []Attr) ([]Attr, error) {
	for i, a := range attrs {
		if a.Name != "user_id" {
			continue
		}
		if a.Type != "string" && a.Type != "text" {
			return nil, fmt.Errorf("user_id is %s, and the actor id is a string; use user_id:string", a.Type)
		}
		attrs[i].Owner = true
		return attrs, nil
	}
	return append(attrs, Attr{Name: "user_id", Type: "string", Owner: true}), nil
}

// MarkOwner sets Owner on an existing user_id. A controller over a model that
// already exists does not invent the column.
func MarkOwner(attrs []Attr) ([]Attr, error) {
	found := false
	for i, a := range attrs {
		if a.Name != "user_id" {
			continue
		}
		if a.Type != "string" && a.Type != "text" {
			return nil, fmt.Errorf("user_id is %s, and the actor id is a string", a.Type)
		}
		attrs[i].Owner = true
		found = true
	}
	if !found {
		return nil, fmt.Errorf("no user_id string, which is how a policy tells what the actor owns")
	}
	return attrs, nil
}

// HasOwnerField reports whether the model source declares UserID as a string.
func HasOwnerField(src []byte, name string) error {
	fields, err := StructFields(src, Camel(name))
	if err != nil {
		return err
	}
	for _, f := range fields {
		if f.Name != "UserID" {
			continue
		}
		if f.Type != "string" {
			return fmt.Errorf("UserID is %s, and the actor id is a string", f.Type)
		}
		return nil
	}
	return fmt.Errorf("no UserID field")
}

// Policy writes the authorization for one model: the shared actor, the
// model's policy and its table spec, and the service that calls the policy
// before the store. Plain Go, no Bogie import. The model is singular
// (post); the service package is the table (posts).
func Policy(module, name string, scheme IDScheme) ([]File, error) {
	if !namePattern.MatchString(name) {
		return nil, fmt.Errorf("%q is not a model name: lowercase letters, digits and underscores, singular", name)
	}
	if looksPlural(name) {
		return nil, fmt.Errorf("%q looks plural; name the model in the singular (%s), the same name as app/models/%s.go", name, Singular(name), Singular(name))
	}
	files := []File{
		{Path: "app/policies/actor.go", Content: actorFile()},
		{Path: "app/policies/policies_suite_test.go", Content: suiteFile("policies", "Policies")},
		{Path: "app/policies/" + name + "_policy.go", Content: policyFile(module, name)},
		{Path: "app/policies/" + name + "_policy_test.go", Content: policyTest(module, name)},
		{Path: "app/services/" + Plural(name) + "/" + Plural(name) + ".go", Content: policyService(module, name)},
		{Path: "app/services/" + Plural(name) + "/" + Plural(name) + "_suite_test.go", Content: suiteFile(Plural(name), Camel(Plural(name)))},
		{Path: "app/services/" + Plural(name) + "/" + Plural(name) + "_test.go", Content: policyServiceTest(module, name, scheme)},
	}
	for i := range files {
		if !strings.HasSuffix(files[i].Path, ".go") {
			continue
		}
		out, err := format.Source([]byte(files[i].Content))
		if err != nil {
			return nil, fmt.Errorf("policy %s: generated %s does not parse: %w", name, files[i].Path, err)
		}
		files[i].Content = string(out)
	}
	return files, nil
}

// PolicyResourcePaths are the files of one model's policy, for destroy.
// The shared actor stays until the last policy is removed.
func PolicyResourcePaths(name string) []File {
	table := Plural(name)
	dir := "app/services/" + table + "/"
	return []File{
		{Path: "app/policies/" + name + "_policy.go"},
		{Path: "app/policies/" + name + "_policy_test.go"},
		{Path: dir + table + ".go"},
		{Path: dir + table + "_test.go"},
		{Path: dir + table + "_suite_test.go"},
	}
}

// PolicySharedPaths are the actor and the policies suite, written by the
// first policy and removed with the last.
func PolicySharedPaths() []File {
	return []File{
		{Path: "app/policies/actor.go"},
		{Path: "app/policies/policies_suite_test.go"},
	}
}

func actorFile() string {
	return `// Package policies decides who may do what. It is plain Go: an actor and a
// method per action, returning bool. There is no framework, no role table
// and nothing imported from Bogie. The actor is whoever authentication
// already put on the request — an id and a role string — and a policy reads
// those two fields and decides.
package policies

import "errors"

// ErrDenied is a policy that refused. A controller maps it to 403 the same
// way it maps models.ErrNotFound to 404, with errors.Is, in the one place
// that mapping lives.
var ErrDenied = errors.New("denied")

// Actor is who is asking. ID is the subject authentication put on the
// request (a user id, usually) and Role is the role string it put beside
// the subject. Both are already on the request context; nothing here loads
// a user or a role.
type Actor struct {
	ID   string
	Role string
}
`
}

func policyFile(module, name string) string {
	typ := Camel(name) // Post
	// Actions are methods on PostPolicy, not package functions: every policy
	// lives in package policies, and CanShow for a post and CanShow for a
	// note would otherwise be the same function.
	var b strings.Builder
	fmt.Fprintf(&b, "package policies\n\nimport %q\n\n", module+"/app/models")
	fmt.Fprintf(&b, `// %[1]s is the authorization for %[2]s. Call it as policies.%[1]s.CanShow(actor, %[3]s).
var %[1]s %[1]sPolicy

// %[1]sPolicy is one function per action. Each takes the actor and the %[3]s
// and returns whether the actor may do it. The default allows only what the
// actor owns.
type %[1]sPolicy struct{}

`, typ, withArticle(name), name)
	actions := []struct{ fn, verb, extra string }{
		{"CanIndex", "list", " Allowed only when the actor owns it."},
		{"CanShow", "read", " Allowed only when the actor owns it."},
		{"CanCreate", "save", " The service sets UserID from the actor before it asks, so this allows a " + name + " the actor will own and refuses every other."},
		{"CanUpdate", "change", " Allowed only when the actor owns it."},
		{"CanDestroy", "delete", " Allowed only when the actor owns it."},
	}
	for _, a := range actions {
		fmt.Fprintf(&b, "// %s reports whether actor may %s %s.%s\nfunc (%sPolicy) %s(actor Actor, %s *models.%s) bool {\n\treturn %sPolicy{}.owns(actor, %s)\n}\n\n",
			a.fn, a.verb, withArticle(name), a.extra, typ, a.fn, name, typ, typ, name)
	}
	fmt.Fprintf(&b, `// owns reports whether actor owns %[2]s. The owner is %[2]s.UserID, the same
// string authentication records as the subject. An actor with no id owns
// nothing, and a nil %[2]s is not owned by anyone. A role does not widen
// this: an admin owns only their own %[2]s until a method above says so.
func (%[1]sPolicy) owns(actor Actor, %[2]s *models.%[1]s) bool {
	return actor.ID != "" && %[2]s != nil && %[2]s.UserID == actor.ID
}
`, typ, name)
	return b.String()
}

func policyTest(module, name string) string {
	typ := Camel(name)
	var entries strings.Builder
	for _, role := range PolicyRoles {
		for _, action := range ResourceActions {
			fmt.Fprintf(&entries, "\t\tEntry(%q, %q, %q, %q, true),\n",
				role+" may "+action+" their own", role, action, ownerID)
			fmt.Fprintf(&entries, "\t\tEntry(%q, %q, %q, %q, false),\n",
				role+" may not "+action+" another's", role, action, otherID)
		}
	}
	return fmt.Sprintf(`package policies_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	%[1]q
	%[2]q
)

// One row per role and action, for the actor who owns the %[3]s and for one
// who does not. The default allows only the owner; a role is not a grant.
var _ = DescribeTable("%[3]s policy",
	func(role, action, id string, allow bool) {
		actor := policies.Actor{ID: id, Role: role}
		%[3]s := &models.%[4]s{UserID: %[5]q}
		var got bool
		switch action {
		case "index":
			got = policies.%[4]s.CanIndex(actor, %[3]s)
		case "show":
			got = policies.%[4]s.CanShow(actor, %[3]s)
		case "create":
			got = policies.%[4]s.CanCreate(actor, %[3]s)
		case "update":
			got = policies.%[4]s.CanUpdate(actor, %[3]s)
		case "destroy":
			got = policies.%[4]s.CanDestroy(actor, %[3]s)
		default:
			Fail("unknown action " + action)
		}
		Expect(got).To(Equal(allow))
	},
%[6]s)

var _ = Describe("%[3]s policy, at the edges", func() {
	It("denies an actor with no id", func() {
		Expect(policies.%[4]s.CanShow(policies.Actor{ID: "", Role: "member"}, &models.%[4]s{UserID: %[5]q})).To(BeFalse())
	})

	It("denies a nil %[3]s", func() {
		Expect(policies.%[4]s.CanShow(policies.Actor{ID: %[5]q, Role: "member"}, nil)).To(BeFalse())
	})
})
`, module+"/app/models", module+"/app/policies", name, typ, ownerID, entries.String())
}

func policyService(module, name string) string {
	typ := Camel(name)
	table := Plural(name)
	many := Camel(table)
	recv := paramName(name, "s")
	return fmt.Sprintf(`// Package %[1]s is the %[2]s resource's service: it checks the policy, then
// calls the store. It knows nothing about HTTP. A denial is policies.ErrDenied,
// which the controller maps to 403.
package %[1]s

import (
	"context"
	"log/slog"

	%[3]q
	%[4]q
)

// store is the slice of persistence this service uses, declared here, next
// to its caller. *adapter.Store satisfies it.
type store interface {
	Create%[5]s(ctx context.Context, %[6]s models.%[5]s) (models.%[5]s, error)
	Get%[5]s(ctx context.Context, id string) (models.%[5]s, error)
	List%[7]s(ctx context.Context, limit int32) ([]models.%[5]s, error)
	Update%[5]s(ctx context.Context, %[6]s models.%[5]s) (models.%[5]s, error)
	Delete%[5]s(ctx context.Context, id string) error
}

// Service checks the %[2]s policy and then saves or loads.
type Service struct {
	store store
	log   *slog.Logger
}

// New builds the service.
func New(store store, log *slog.Logger) *Service {
	return &Service{store: store, log: log}
}

func (s *Service) deny(ctx context.Context, action string, actor policies.Actor) {
	s.log.InfoContext(ctx, "%[2]s: denied", "action", action, "actor", actor.ID, "role", actor.Role)
}

// Index returns the %[1]s the actor may list: the ones they own.
func (s *Service) Index(ctx context.Context, actor policies.Actor, limit int32) ([]models.%[5]s, error) {
	%[1]s, err := s.store.List%[7]s(ctx, limit)
	if err != nil {
		return nil, err
	}
	owned := make([]models.%[5]s, 0, len(%[1]s))
	for i := range %[1]s {
		if policies.%[5]s.CanIndex(actor, &%[1]s[i]) {
			owned = append(owned, %[1]s[i])
		}
	}
	return owned, nil
}

// Show returns the %[2]s, or policies.ErrDenied when the actor may not read it.
// A missing %[2]s is models.ErrNotFound, so a caller can tell the two apart.
func (s *Service) Show(ctx context.Context, actor policies.Actor, id string) (models.%[5]s, error) {
	%[6]s, err := s.store.Get%[5]s(ctx, id)
	if err != nil {
		return models.%[5]s{}, err
	}
	if !policies.%[5]s.CanShow(actor, &%[6]s) {
		s.deny(ctx, "show", actor)
		return models.%[5]s{}, policies.ErrDenied
	}
	return %[6]s, nil
}

// Create saves %[6]s with the actor as its owner. The body cannot choose a
// different owner: UserID is set from the actor before the policy is asked.
func (s *Service) Create(ctx context.Context, actor policies.Actor, %[6]s models.%[5]s) (models.%[5]s, error) {
	%[6]s.UserID = actor.ID
	if !policies.%[5]s.CanCreate(actor, &%[6]s) {
		s.deny(ctx, "create", actor)
		return models.%[5]s{}, policies.ErrDenied
	}
	return s.store.Create%[5]s(ctx, %[6]s)
}

// Update saves %[6]s when the actor may change it. UserID is the one the
// record already had; the caller keeps it.
func (s *Service) Update(ctx context.Context, actor policies.Actor, %[6]s models.%[5]s) (models.%[5]s, error) {
	if !policies.%[5]s.CanUpdate(actor, &%[6]s) {
		s.deny(ctx, "update", actor)
		return models.%[5]s{}, policies.ErrDenied
	}
	return s.store.Update%[5]s(ctx, %[6]s)
}

// Destroy deletes the %[2]s when the actor owns it.
func (s *Service) Destroy(ctx context.Context, actor policies.Actor, id string) error {
	%[6]s, err := s.store.Get%[5]s(ctx, id)
	if err != nil {
		return err
	}
	if !policies.%[5]s.CanDestroy(actor, &%[6]s) {
		s.deny(ctx, "destroy", actor)
		return policies.ErrDenied
	}
	return s.store.Delete%[5]s(ctx, id)
}
`, table, name, module+"/app/models", module+"/app/policies", typ, recv, many)
}

func policyServiceTest(module, name string, scheme IDScheme) string {
	typ := Camel(name)
	table := Plural(name)
	recv := paramName(name, "f")
	idField := "int64(1)"
	idText := "1"
	if scheme == IDUUID {
		idField = `"` + "11111111-1111-4111-8111-111111111111" + `"`
		idText = "11111111-1111-4111-8111-111111111111"
	}
	return fmt.Sprintf(`package %[1]s_test

import (
	"context"
	"io"
	"log/slog"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	%[2]q
	%[3]q
	%[4]q
)

// fakeStore is the store interface the service declares, in memory.
type fakeStore struct {
	%[6]s models.%[5]s
	saved bool
}

func (f *fakeStore) Create%[5]s(_ context.Context, %[6]s models.%[5]s) (models.%[5]s, error) {
	%[6]s.ID = %[7]s
	f.%[6]s = %[6]s
	f.saved = true
	return %[6]s, nil
}

func (f *fakeStore) Get%[5]s(_ context.Context, id string) (models.%[5]s, error) {
	if !f.saved || id != %[8]q {
		return models.%[5]s{}, models.ErrNotFound
	}
	return f.%[6]s, nil
}

func (f *fakeStore) List%[9]s(_ context.Context, _ int32) ([]models.%[5]s, error) {
	if !f.saved {
		return nil, nil
	}
	return []models.%[5]s{f.%[6]s}, nil
}

func (f *fakeStore) Update%[5]s(_ context.Context, %[6]s models.%[5]s) (models.%[5]s, error) {
	f.%[6]s = %[6]s
	return %[6]s, nil
}

func (f *fakeStore) Delete%[5]s(_ context.Context, id string) error {
	if !f.saved || id != %[8]q {
		return models.ErrNotFound
	}
	f.saved = false
	return nil
}

var _ = Describe("Service", func() {
	var (
		fake *fakeStore
		svc  *%[1]s.Service
	)

	BeforeEach(func() {
		fake = &fakeStore{saved: true, %[6]s: models.%[5]s{ID: %[7]s, UserID: %[10]q}}
		svc = %[1]s.New(fake, slog.New(slog.NewTextHandler(io.Discard, nil)))
	})

	Describe("Show", func() {
		It("returns the %[11]s to its owner", func() {
			got, err := svc.Show(context.Background(), policies.Actor{ID: %[10]q, Role: "member"}, %[8]q)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.UserID).To(Equal(%[10]q))
		})

		It("denies someone else", func() {
			_, err := svc.Show(context.Background(), policies.Actor{ID: %[12]q, Role: "member"}, %[8]q)
			Expect(err).To(MatchError(policies.ErrDenied))
		})

		It("is not found when nothing has that id", func() {
			_, err := svc.Show(context.Background(), policies.Actor{ID: %[10]q, Role: "member"}, "missing")
			Expect(err).To(MatchError(models.ErrNotFound))
		})
	})

	Describe("Create", func() {
		It("stamps the actor as the owner", func() {
			got, err := svc.Create(context.Background(), policies.Actor{ID: %[10]q, Role: "member"}, models.%[5]s{})
			Expect(err).NotTo(HaveOccurred())
			Expect(got.UserID).To(Equal(%[10]q))
		})

		It("denies an actor with no id", func() {
			_, err := svc.Create(context.Background(), policies.Actor{Role: "member"}, models.%[5]s{})
			Expect(err).To(MatchError(policies.ErrDenied))
		})
	})
})
`, table, module+"/app/models", module+"/app/policies", module+"/app/services/"+table, typ, recv, idField, idText, Camel(table), ownerID, name, otherID)
}
