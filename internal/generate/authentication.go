package generate

import (
	"fmt"
	"go/format"
	"strings"
	"time"
)

// AuthenticationShapes are what `bogie g authentication SHAPE` writes, each
// a middleware on a route group, as the reference services guard theirs:
//
//	secret   a shared secret in a header, for the Rails app calling this
//	         service over the private network (kchat's control secret)
//	token    bearer tokens, JWT HS256 with a shared secret, for a service
//	         that faces users or devices; the Rails app can mint them with
//	         the jwt gem and this service verifies (line-connect's sessions)
//	api_key  keys with a bcrypt digest in an api_keys table, for machine
//	         clients (line-connect's applications)
var AuthenticationShapes = []string{"secret", "token", "api_key"}

// Setting is a configuration key a shape adds: its credentials-file key
// under the app's section, the environment variable that overrides it, and
// the Go field on AppConfig.
type Setting struct {
	Key   string // control_secret
	Env   string // BLOG_CONTROL_SECRET
	Field string // ControlSecret
	Min   int    // required length in production
}

// AuthenticationSettings are the settings a shape reads from config, so the
// command can write development values into the credentials file.
func AuthenticationSettings(name, shape string) []Setting {
	prefix := strings.ToUpper(name)
	switch shape {
	case "secret":
		return []Setting{{"control_secret", prefix + "_CONTROL_SECRET", "ControlSecret", 32}}
	case "token":
		return []Setting{{"token_secret", prefix + "_TOKEN_SECRET", "TokenSecret", 32}}
	}
	return nil
}

// Authentication writes one shape: the middleware and its test, the route
// group helper on the root Server, and for api_key the model, the service
// and the CLI command; and the lines that register it, in config.go for a
// setting, in the root wiring for a service, in main.go for a command.
func Authentication(module, name, shape string, now time.Time) ([]File, []Wire, error) {
	a := auth{module: module, name: name}
	var files []File
	var wires []Wire
	var err error
	switch shape {
	case "secret":
		files, wires, err = a.secret()
	case "token":
		files, wires, err = a.token()
	case "api_key":
		files, wires, err = a.apiKey(now)
	default:
		return nil, nil, fmt.Errorf("%q is not an authentication shape; one of %s", shape, strings.Join(AuthenticationShapes, ", "))
	}
	if err != nil {
		return nil, nil, err
	}
	// Table alignment in the tests is gofmt's job, not the template's.
	for i := range files {
		if !strings.HasSuffix(files[i].Path, ".go") {
			continue
		}
		out, err := format.Source([]byte(files[i].Content))
		if err != nil {
			return nil, nil, fmt.Errorf("authentication %s: generated %s does not parse: %w", shape, files[i].Path, err)
		}
		files[i].Content = string(out)
	}
	return files, wires, nil
}

// AuthenticationWirePrefixes are what destroy removes for a shape.
func AuthenticationWirePrefixes(name, shape string) []Wire {
	a := auth{name: name}
	var ws []Wire
	for _, s := range AuthenticationSettings(name, shape) {
		ws = append(ws, a.settingWires(s)...)
	}
	if shape == "api_key" {
		ws = append(ws,
			Wire{File: "app/controllers/application.go", Line: "APIKeys *api_keys.Service"},
			Wire{File: "app/application.go", Line: "server.APIKeys ="},
			Wire{File: "main.go", Line: `"api_keys":`},
		)
	}
	for i := range ws {
		// A prefix is the start of the line up to the value.
		if j := strings.Index(ws[i].Line, " = envOr("); j > 0 {
			ws[i].Line = ws[i].Line[:j+len(" = envOr(")]
		}
	}
	return ws
}

type auth struct{ module, name string }

func (a auth) header() string { return "X-" + Camel(a.name) + "-Secret" }

// settingWires are the three lines a setting needs in config.go: the field,
// the environment override, and the production check.
func (a auth) settingWires(s Setting) []Wire {
	return []Wire{
		{File: "config/config.go", Marker: "config", Line: fmt.Sprintf("%s string `mapstructure:%q`", s.Field, s.Key)},
		{File: "config/config.go", Marker: "env", Line: fmt.Sprintf("a.%s = envOr(%q, a.%s)", s.Field, s.Env, s.Field)},
		{File: "config/config.go", Marker: "validate", Line: fmt.Sprintf("requireSecret(c.Env, %q, a.%s, %d),", s.Env, s.Field, s.Min)},
	}
}

func (a auth) secret() ([]File, []Wire, error) {
	setting := AuthenticationSettings(a.name, "secret")[0]
	files := []File{
		{Path: "app/middlewares/require_secret.go", Content: fmt.Sprintf(`package middlewares

import (
	"crypto/subtle"
	"net/http"

	"github.com/gin-gonic/gin"
)

// SecretHeader carries the shared secret on the routes the Rails app calls.
const SecretHeader = %[1]q

// RequireSecret guards routes for a trusted caller: the Rails app next door,
// over the private network. This is a shared secret, not a user credential.
// Anything that needs to know WHICH person is acting carries that in the
// request body, because the permission check already happened in Rails.
//
// The secret comes from config (%[2]s.%[3]s in the credentials file,
// %[4]s in the environment); an empty one opens nothing.
func RequireSecret(secret string) gin.HandlerFunc {
	want := []byte(secret)
	return func(c *gin.Context) {
		got := []byte(c.GetHeader(SecretHeader))

		// Basic auth as a fallback, so a page can be opened in a browser,
		// which cannot send a custom header. Any username; the password is
		// the secret.
		if len(got) == 0 {
			if _, password, ok := c.Request.BasicAuth(); ok {
				got = []byte(password)
			}
		}

		// Constant time, and length-checked first so the compare cannot be
		// short-circuited by a differing length.
		if len(want) == 0 || len(got) != len(want) || subtle.ConstantTimeCompare(got, want) != 1 {
			// Prompt rather than a bare 401, so a browser offers a login box
			// instead of a blank page.
			c.Header("WWW-Authenticate", `+"`"+`Basic realm=%[2]q, charset="UTF-8"`+"`"+`)
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Next()
	}
}
`, a.header(), a.name, setting.Key, setting.Env)},
		{Path: "app/middlewares/require_secret_test.go", Content: fmt.Sprintf(`package middlewares

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func secretRequest(t *testing.T, secret string, set func(*http.Request)) int {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/guarded", RequireSecret(secret), func(c *gin.Context) { c.Status(http.StatusOK) })
	req := httptest.NewRequest(http.MethodGet, "/guarded", nil)
	set(req)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec.Code
}

func TestRequireSecret(t *testing.T) {
	const secret = "a-secret-that-is-at-least-thirty-two-bytes"
	cases := map[string]struct {
		configured string
		set        func(*http.Request)
		want       int
	}{
		"the header opens":          {secret, func(r *http.Request) { r.Header.Set(SecretHeader, secret) }, http.StatusOK},
		"basic auth opens":          {secret, func(r *http.Request) { r.SetBasicAuth("anyone", secret) }, http.StatusOK},
		"nothing is refused":        {secret, func(*http.Request) {}, http.StatusUnauthorized},
		"a wrong secret is refused": {secret, func(r *http.Request) { r.Header.Set(SecretHeader, "nope") }, http.StatusUnauthorized},
		"a prefix is refused":       {secret, func(r *http.Request) { r.Header.Set(SecretHeader, secret[:10]) }, http.StatusUnauthorized},
		"an empty secret opens nothing": {"", func(r *http.Request) { r.Header.Set(SecretHeader, "") }, http.StatusUnauthorized},
	}
	for name, tc := range cases {
		if got := secretRequest(t, tc.configured, tc.set); got != tc.want {
			t.Errorf("%%s: %%d, want %%d", name, got, tc.want)
		}
	}
}
`)},
		{Path: "app/controllers/authentication_secret.go", Content: fmt.Sprintf(`package controllers

import (
	"github.com/gin-gonic/gin"

	%[1]q
)

// WithSecret is the route group the Rails app calls. Every route mounted in
// it needs the shared secret from config (%[2]s.%[3]s in the credentials
// file, %[4]s in the environment) in the %[5]s header. Mount a
// controller here instead of on the router, in routes.go:
//
//	s.Reports.SetupRoutes(s.WithSecret(&r.RouterGroup))
//
// On the Rails side, one header on the client:
//
//	Faraday.new(url: ENV["%[6]s_URL"], headers: { "%[5]s" => Rails.application.credentials.%[2]s_secret })
func (s *Server) WithSecret(r *gin.RouterGroup) *gin.RouterGroup {
	return r.Group("", middlewares.RequireSecret(s.Config.App.%[7]s))
}
`, a.module+"/app/middlewares", a.name, setting.Key, setting.Env, a.header(), strings.ToUpper(a.name), setting.Field)},
	}
	return files, a.settingWires(setting), nil
}

func (a auth) token() ([]File, []Wire, error) {
	setting := AuthenticationSettings(a.name, "token")[0]
	files := []File{
		{Path: "app/services/token/token.go", Content: fmt.Sprintf(`// Package token mints and verifies the bearer tokens a signed-in user
// presents. JWT, HS256, one shared secret: the Rails app next door can mint
// a token for its user with the jwt gem and this service verifies it, so
// users keep living in Rails, or this service mints its own from a sessions
// controller. Nothing here knows about HTTP.
package token

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// ErrInvalid is a token that does not verify: wrong signature, wrong
// issuer, malformed. ErrExpired is one that did, but too late.
var (
	ErrInvalid = errors.New("token: invalid")
	ErrExpired = errors.New("token: expired")
)

// Maker holds the secret and the issuer every token must carry. The secret
// is %[1]s.%[2]s in the credentials file (%[3]s in the environment), at
// least 32 bytes; the issuer is the app's name, so a token minted for
// another service with the same secret is still refused.
type Maker struct {
	Secret []byte
	Issuer string
}

// Claims is what a verified token says.
type Claims struct {
	// Subject is who the token was minted for: the Rails user's id, usually.
	Subject   string
	ExpiresAt time.Time
	// ID is the token's own id, for a revocation list if you keep one.
	ID string
}

// Mint signs a token for subject that expires after ttl.
func (m Maker) Mint(subject string, ttl time.Duration) (string, error) {
	if subject == "" {
		return "", fmt.Errorf("%%w: empty subject", ErrInvalid)
	}
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Issuer:    m.Issuer,
		Subject:   subject,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		ID:        uuid.NewString(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.Secret)
}

// Verify checks the signature, the issuer and the expiry, and returns the
// claims. The method is pinned to HS256 so a token claiming "none" or an
// asymmetric algorithm is refused before the key is consulted.
func (m Maker) Verify(raw string) (Claims, error) {
	var claims jwt.RegisteredClaims
	_, err := jwt.ParseWithClaims(raw, &claims, func(*jwt.Token) (any, error) { return m.Secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(m.Issuer),
		jwt.WithExpirationRequired(),
	)
	switch {
	case errors.Is(err, jwt.ErrTokenExpired):
		return Claims{}, ErrExpired
	case err != nil:
		return Claims{}, ErrInvalid
	case claims.Subject == "":
		return Claims{}, ErrInvalid
	}
	return Claims{Subject: claims.Subject, ExpiresAt: claims.ExpiresAt.Time, ID: claims.ID}, nil
}
`, a.name, setting.Key, setting.Env)},
		{Path: "app/services/token/token_test.go", Content: `package token

import (
	"errors"
	"testing"
	"time"
)

var maker = Maker{Secret: []byte("a-test-secret-of-at-least-thirty-two-bytes"), Issuer: "test"}

func TestMintThenVerify(t *testing.T) {
	raw, err := maker.Mint("user-42", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := maker.Verify(raw)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != "user-42" || claims.ID == "" || time.Until(claims.ExpiresAt) <= 0 {
		t.Errorf("claims = %+v", claims)
	}
}

func TestVerifyRefuses(t *testing.T) {
	expired, _ := maker.Mint("user-42", -time.Minute)
	otherSecret, _ := Maker{Secret: []byte("another-secret-of-at-least-thirty-two-bytes"), Issuer: "test"}.Mint("user-42", time.Minute)
	otherIssuer, _ := Maker{Secret: maker.Secret, Issuer: "someone-else"}.Mint("user-42", time.Minute)

	for name, tc := range map[string]struct {
		raw  string
		want error
	}{
		"expired":        {expired, ErrExpired},
		"another secret": {otherSecret, ErrInvalid},
		"another issuer": {otherIssuer, ErrInvalid},
		"garbage":        {"not.a.token", ErrInvalid},
		"empty":          {"", ErrInvalid},
	} {
		if _, err := maker.Verify(tc.raw); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", name, err, tc.want)
		}
	}
	if _, err := maker.Mint("", time.Minute); err == nil {
		t.Error("minted a token with no subject")
	}
}
`},
		{Path: "app/middlewares/require_user.go", Content: fmt.Sprintf(`package middlewares

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	%[1]q
	%[2]q
)

// userTokens is the slice of the token service this middleware uses,
// declared here (rule 2). token.Maker satisfies it.
type userTokens interface {
	Verify(raw string) (token.Claims, error)
}

const subjectKey = "subject"

// RequireUser guards routes for a signed-in user: a bearer token in the
// Authorization header that the token service verifies. The token's subject
// is then on the context for handlers, through Subject. Who the subject is,
// a Rails user id usually, is for the caller and the handler to agree on;
// nothing is loaded here.
func RequireUser(tokens userTokens) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
		if !ok || raw == "" {
			c.Header("WWW-Authenticate", `+"`"+`Bearer realm=%[3]q`+"`"+`)
			c.AbortWithStatusJSON(http.StatusUnauthorized, views.ErrorMessage("a bearer token is required"))
			return
		}
		claims, err := tokens.Verify(raw)
		if err != nil {
			c.Header("WWW-Authenticate", `+"`"+`Bearer realm=%[3]q, error="invalid_token"`+"`"+`)
			c.AbortWithStatusJSON(http.StatusUnauthorized, views.Error(err))
			return
		}
		c.Set(subjectKey, claims.Subject)
		c.Next()
	}
}

// Subject is who the request's token was minted for, or "" on a route that
// RequireUser does not guard.
func Subject(c *gin.Context) string { return c.GetString(subjectKey) }
`, a.module+"/app/services/token", a.module+"/app/views", a.name)},
		{Path: "app/middlewares/require_user_test.go", Content: fmt.Sprintf(`package middlewares

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	%[1]q
)

var testTokens = token.Maker{Secret: []byte("a-test-secret-of-at-least-thirty-two-bytes"), Issuer: "test"}

func userRequest(t *testing.T, authorization string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/me", RequireUser(testTokens), func(c *gin.Context) { c.String(http.StatusOK, Subject(c)) })
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestRequireUser(t *testing.T) {
	good, _ := testTokens.Mint("user-42", time.Minute)
	expired, _ := testTokens.Mint("user-42", -time.Minute)

	if rec := userRequest(t, "Bearer "+good); rec.Code != http.StatusOK || rec.Body.String() != "user-42" {
		t.Errorf("good token: %%d %%q", rec.Code, rec.Body.String())
	}
	for name, header := range map[string]string{
		"none": "", "expired": "Bearer " + expired, "garbage": "Bearer nope", "wrong scheme": "Basic " + good,
	} {
		if rec := userRequest(t, header); rec.Code != http.StatusUnauthorized {
			t.Errorf("%%s: %%d, want 401", name, rec.Code)
		}
	}
}
`, a.module+"/app/services/token")},
		{Path: "app/controllers/authentication_token.go", Content: fmt.Sprintf(`package controllers

import (
	"github.com/gin-gonic/gin"

	%[1]q
	%[2]q
)

// Tokens is the token maker for this app: the secret from config
// (%[3]s.%[4]s in the credentials file, %[5]s in the environment) and the
// app's name as issuer. A sessions controller that signs users in here mints
// with it; a Rails app that signs them in mints the same token with the jwt
// gem and the same secret:
//
//	JWT.encode({ iss: %[3]q, sub: user.id.to_s, iat: Time.now.to_i, exp: 15.minutes.from_now.to_i, jti: SecureRandom.uuid },
//	           Rails.application.credentials.%[3]s_token_secret, "HS256")
func (s *Server) Tokens() token.Maker {
	return token.Maker{Secret: []byte(s.Config.App.%[6]s), Issuer: %[3]q}
}

// WithUser is the route group a signed-in user is required for. Mount a
// controller here instead of on the router, in routes.go:
//
//	s.Posts.SetupRoutes(s.WithUser(&r.RouterGroup))
//
// and in a handler, middlewares.Subject(c) is the token's subject.
func (s *Server) WithUser(r *gin.RouterGroup) *gin.RouterGroup {
	return r.Group("", middlewares.RequireUser(s.Tokens()))
}
`, a.module+"/app/middlewares", a.module+"/app/services/token", a.name, setting.Key, setting.Env, setting.Field)},
	}
	return files, a.settingWires(setting), nil
}

func (a auth) apiKey(now time.Time) ([]File, []Wire, error) {
	attrs, err := ParseAttrs([]string{"name:string", "prefix:string:uniq", "digest:string"})
	if err != nil {
		return nil, nil, err
	}
	files, err := Model(a.module, "api_key", attrs, now)
	if err != nil {
		return nil, nil, err
	}
	// One query the model generator does not write: the lookup a presented
	// key is checked with, by its public prefix.
	for i := range files {
		switch files[i].Path {
		case "db/queries/api_keys.sql":
			files[i].Content += "\n-- name: GetApiKeyByPrefix :one\nSELECT * FROM api_keys WHERE prefix = $1;\n"
		case "app/models/api_keys.go":
			files[i].Content += `
// GetApiKeyByPrefix finds the key a presented "<prefix>.<secret>" names, by
// its prefix; the service compares the secret against the digest.
func (s *Store) GetApiKeyByPrefix(ctx context.Context, prefix string) (domain.ApiKey, error) {
	row, err := s.q.GetApiKeyByPrefix(ctx, prefix)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return domain.ApiKey{}, domain.ErrNotFound
	case err != nil:
		return domain.ApiKey{}, fmt.Errorf("models: get api key by prefix: %w", err)
	}
	return api_keyFromRow(row), nil
}
`
		}
	}

	files = append(files,
		File{Path: "app/services/api_keys/api_keys.go", Content: fmt.Sprintf(`// Package api_keys makes and checks the keys machine clients present: a
// public prefix that finds the row and a secret that is stored only as a
// bcrypt digest, so the table leaks nothing if it leaks. A key is shown in
// full once, when it is made. Nothing here knows about HTTP.
package api_keys

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"

	%[1]q
)

// ErrInvalid is a presented key that names no row or does not match it;
// a caller cannot tell the two apart.
var ErrInvalid = errors.New("api key: invalid")

// store is the slice of the store this service uses (rule 2).
type store interface {
	CreateApiKey(ctx context.Context, k domain.ApiKey) (domain.ApiKey, error)
	GetApiKeyByPrefix(ctx context.Context, prefix string) (domain.ApiKey, error)
}

// Service makes and verifies keys.
type Service struct {
	Store store
}

// New builds the service.
func New(s store) *Service { return &Service{Store: s} }

// Create makes a key for name and returns it in full, "<prefix>.<secret>",
// the only time the secret is ever seen; and the row that was stored.
func (s *Service) Create(ctx context.Context, name string) (string, domain.ApiKey, error) {
	prefix, err := random(6)
	if err != nil {
		return "", domain.ApiKey{}, err
	}
	secret, err := random(24)
	if err != nil {
		return "", domain.ApiKey{}, err
	}
	digest, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
	if err != nil {
		return "", domain.ApiKey{}, fmt.Errorf("api keys: digest: %%w", err)
	}
	key, err := s.Store.CreateApiKey(ctx, domain.ApiKey{Name: name, Prefix: prefix, Digest: string(digest)})
	if err != nil {
		return "", domain.ApiKey{}, err
	}
	return prefix + "." + secret, key, nil
}

// Verify checks a presented key and returns the row it names.
func (s *Service) Verify(ctx context.Context, presented string) (domain.ApiKey, error) {
	prefix, secret, ok := strings.Cut(presented, ".")
	if !ok || prefix == "" || secret == "" {
		return domain.ApiKey{}, ErrInvalid
	}
	key, err := s.Store.GetApiKeyByPrefix(ctx, prefix)
	if err != nil {
		return domain.ApiKey{}, ErrInvalid
	}
	if bcrypt.CompareHashAndPassword([]byte(key.Digest), []byte(secret)) != nil {
		return domain.ApiKey{}, ErrInvalid
	}
	return key, nil
}

func random(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("api keys: random: %%w", err)
	}
	return hex.EncodeToString(b), nil
}
`, a.module+"/app/domain")},
		File{Path: "app/services/api_keys/api_keys_test.go", Content: fmt.Sprintf(`package api_keys

import (
	"context"
	"errors"
	"testing"

	%[1]q
)

// fakeStore keeps keys in memory, by prefix.
type fakeStore struct{ keys map[string]domain.ApiKey }

func (f *fakeStore) CreateApiKey(_ context.Context, k domain.ApiKey) (domain.ApiKey, error) {
	k.ID = "id-" + k.Prefix
	f.keys[k.Prefix] = k
	return k, nil
}

func (f *fakeStore) GetApiKeyByPrefix(_ context.Context, prefix string) (domain.ApiKey, error) {
	k, ok := f.keys[prefix]
	if !ok {
		return domain.ApiKey{}, domain.ErrNotFound
	}
	return k, nil
}

func TestCreateThenVerify(t *testing.T) {
	svc := New(&fakeStore{keys: map[string]domain.ApiKey{}})
	ctx := context.Background()

	presented, made, err := svc.Create(ctx, "billing")
	if err != nil {
		t.Fatal(err)
	}
	if made.Name != "billing" || made.Digest == "" || made.Digest == presented {
		t.Errorf("stored %%+v for key %%q", made, presented)
	}
	got, err := svc.Verify(ctx, presented)
	if err != nil || got.ID != made.ID {
		t.Errorf("Verify = %%+v, %%v", got, err)
	}
	for name, bad := range map[string]string{
		"wrong secret":  made.Prefix + ".nope",
		"wrong prefix":  "nope." + presented[len(made.Prefix)+1:],
		"no dot":        "nope",
		"empty":         "",
	} {
		if _, err := svc.Verify(ctx, bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%%s: err = %%v, want ErrInvalid", name, err)
		}
	}
}
`, a.module+"/app/domain")},
		File{Path: "app/middlewares/require_api_key.go", Content: fmt.Sprintf(`package middlewares

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	%[1]q
	%[2]q
)

// apiKeys is the slice of the api_keys service this middleware uses,
// declared here (rule 2).
type apiKeys interface {
	Verify(ctx context.Context, presented string) (domain.ApiKey, error)
}

const apiKeyNameKey = "api_key"

// RequireAPIKey guards routes for a machine client: a key made by
// "%[3]s api_keys create NAME", presented as a bearer token. The key's name
// is then on the context for handlers, through APIKeyName.
func RequireAPIKey(keys apiKeys) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
		if !ok || raw == "" {
			c.Header("WWW-Authenticate", `+"`"+`Bearer realm=%[4]q`+"`"+`)
			c.AbortWithStatusJSON(http.StatusUnauthorized, views.ErrorMessage("an api key is required"))
			return
		}
		key, err := keys.Verify(c.Request.Context(), raw)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, views.ErrorMessage("invalid api key"))
			return
		}
		c.Set(apiKeyNameKey, key.Name)
		c.Next()
	}
}

// APIKeyName is the name of the key the request presented, or "" on a route
// that RequireAPIKey does not guard.
func APIKeyName(c *gin.Context) string { return c.GetString(apiKeyNameKey) }
`, a.module+"/app/domain", a.module+"/app/views", a.name, a.name)},
		File{Path: "app/middlewares/require_api_key_test.go", Content: fmt.Sprintf(`package middlewares

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	%[1]q
)

type fakeAPIKeys struct{}

func (fakeAPIKeys) Verify(_ context.Context, presented string) (domain.ApiKey, error) {
	if presented == "good.key" {
		return domain.ApiKey{Name: "billing"}, nil
	}
	return domain.ApiKey{}, errors.New("invalid")
}

func apiKeyRequest(t *testing.T, authorization string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/guarded", RequireAPIKey(fakeAPIKeys{}), func(c *gin.Context) { c.String(http.StatusOK, APIKeyName(c)) })
	req := httptest.NewRequest(http.MethodGet, "/guarded", nil)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestRequireAPIKey(t *testing.T) {
	if rec := apiKeyRequest(t, "Bearer good.key"); rec.Code != http.StatusOK || rec.Body.String() != "billing" {
		t.Errorf("good key: %%d %%q", rec.Code, rec.Body.String())
	}
	for name, header := range map[string]string{"none": "", "wrong": "Bearer bad.key", "wrong scheme": "Basic good.key"} {
		if rec := apiKeyRequest(t, header); rec.Code != http.StatusUnauthorized {
			t.Errorf("%%s: %%d, want 401", name, rec.Code)
		}
	}
}
`, a.module+"/app/domain")},
		File{Path: "app/controllers/authentication_api_key.go", Content: fmt.Sprintf(`package controllers

import (
	"github.com/gin-gonic/gin"

	%[1]q
)

// WithAPIKey is the route group a machine client's key is required for.
// Keys are made with `+"`%[2]s api_keys create NAME`"+` (bogie api_keys:create NAME)
// and presented as a bearer token. Mount a controller here instead of on
// the router, in routes.go:
//
//	s.Reports.SetupRoutes(s.WithAPIKey(&r.RouterGroup))
//
// and in a handler, middlewares.APIKeyName(c) says which key it was.
func (s *Server) WithAPIKey(r *gin.RouterGroup) *gin.RouterGroup {
	return r.Group("", middlewares.RequireAPIKey(s.APIKeys))
}
`, a.module+"/app/middlewares", a.name)},
		File{Path: "cli_api_keys.go", Content: fmt.Sprintf(`package main

import (
	"context"
	"fmt"
	"os"

	%[1]q
	%[2]q
	%[3]q
)

const apiKeysUsage = `+"`"+`Usage: %[4]s api_keys create NAME

Makes an api key for a machine client and prints it, the only time it is
shown in full. The client presents it as a bearer token on the routes
mounted under WithAPIKey. The table keeps a bcrypt digest, never the key.
Spelled the Rails way through the tool: bogie api_keys:create NAME
`+"`"+`

// runAPIKeys handles `+"`%[4]s api_keys ...`"+`.
func runAPIKeys(ctx context.Context, args []string) error {
	if len(args) != 2 || args[0] != "create" {
		fmt.Fprint(os.Stderr, apiKeysUsage)
		return fmt.Errorf("api_keys: create NAME is the one command")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	store, err := models.Open(ctx, cfg.App.DatabaseURL)
	if err != nil {
		return err
	}
	defer store.Close()
	key, made, err := api_keys.New(store).Create(ctx, args[1])
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "api key %%q made (%%s); it is shown once:\n", made.Name, made.ID)
	fmt.Println(key)
	return nil
}
`, a.module+"/app/models", a.module+"/app/services/api_keys", a.module+"/config", a.name)},
	)

	wires := []Wire{
		{File: "app/controllers/application.go", Marker: "controllers", Line: "APIKeys *api_keys.Service", Import: a.module + "/app/services/api_keys"},
		{File: "app/application.go", Marker: "wire", Line: "server.APIKeys = api_keys.New(store)", Import: a.module + "/app/services/api_keys"},
		{File: "main.go", Marker: "commands", Line: `"api_keys": runAPIKeys,`},
	}
	return files, wires, nil
}

// AuthenticationGroup is the route group helper a shape adds to the root
// Server, for the note the command prints.
func AuthenticationGroup(shape string) string {
	switch shape {
	case "secret":
		return "WithSecret"
	case "token":
		return "WithUser"
	default:
		return "WithAPIKey"
	}
}

// AuthenticationFiles are the paths a shape writes, for destroy.
func AuthenticationFiles(module, name, shape string) ([]File, error) {
	files, _, err := Authentication(module, name, shape, time.Time{})
	return files, err
}
