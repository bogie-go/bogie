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
//	secret   a shared secret in a header, for a trusted service calling this
//	         service over the private network (kchat's control secret)
//	token    bearer tokens, JWT HS256 with a shared secret, for a service
//	         that faces users or devices; another service can mint them
//	         with any JWT library and this service verifies (line-connect's sessions)
//	api_key  keys with a bcrypt digest in an api_keys table, for machine
//	         clients (line-connect's applications)
var AuthenticationShapes = []string{"secret", "token", "api_key"}

// Setting is the one secret a shape reads from config: the group of the
// credentials file it sits in and its key, like token and secret, so the file
// has token.secret and the environment TOKEN_SECRET.
type Setting struct {
	Group string // token
	Key   string // secret
	Min   int    // shortest accepted length in production
}

// Path is the key in the credentials file: token.secret.
func (s Setting) Path() string { return s.Group + "." + s.Key }

// Env is the environment variable that overrides it: TOKEN_SECRET.
func (s Setting) Env() string { return strings.ToUpper(s.Group + "_" + s.Key) }

// Field is how config reaches it: Token.Secret, as in s.Config.Token.Secret.
func (s Setting) Field() string { return Camel(s.Group) + "." + Camel(s.Key) }

// ConfigKey is the key in the shape's config group.
func (s Setting) ConfigKey() ConfigKey {
	return ConfigKey{Name: s.Key, Type: "string", Secret: true, Min: s.Min}
}

// AuthenticationSetting is the setting a shape reads from config, so the
// command can add its group and write a development value into the
// credentials. api_key reads none: its keys live in a table.
func AuthenticationSetting(shape string) (Setting, bool) {
	switch shape {
	case "secret":
		return Setting{Group: "control", Key: "secret", Min: 32}, true
	case "token":
		return Setting{Group: "token", Key: "secret", Min: 32}, true
	}
	return Setting{}, false
}

// Authentication writes one shape: the middleware and its test, the route
// group helper on the root Server, and for api_key the model, the service
// and the CLI command; and the lines that register it, in config.go for a
// setting, in the root wiring for a service, in main.go for a command.
func Authentication(module, name, shape string, scheme IDScheme, now time.Time) ([]File, []Wire, error) {
	a := auth{module: module, name: name, scheme: scheme}
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

// AuthenticationWirePrefixes are what destroy removes for a shape, apart from
// its config group, which destroy removes as `d config` does.
func AuthenticationWirePrefixes(name, shape string) []Wire {
	var ws []Wire
	if shape == "api_key" {
		ws = append(ws,
			Wire{File: "app/controllers/application.go", Line: "APIKeys *api_keys.Service"},
			Wire{File: "app/application.go", Line: "server.APIKeys ="},
			Wire{File: "main.go", Line: `"api_keys":`},
		)
	}
	return ws
}

type auth struct {
	module, name string
	scheme       IDScheme
}

func (a auth) header() string { return "X-" + Camel(a.name) + "-Secret" }

func (a auth) secret() ([]File, []Wire, error) {
	setting, _ := AuthenticationSetting("secret")
	files := []File{
		{Path: "app/middlewares/require_secret.go", Content: fmt.Sprintf(`package middlewares

import (
	"crypto/subtle"
	"net/http"

	"github.com/gin-gonic/gin"
)

// SecretHeader carries the shared secret on the routes a trusted service calls.
const SecretHeader = %[1]q

// RequireSecret guards routes for a trusted caller: another service next door,
// over the private network. This is a shared secret, not a user credential.
// Anything that needs to know WHICH person is acting carries that in the
// request body, because the permission check already happened in the caller.
//
// The secret comes from config (%[3]s in the credentials file,
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
`, a.header(), a.name, setting.Path(), setting.Env())},
		{Path: "app/middlewares/require_secret_test.go", Content: fmt.Sprintf(`package middlewares_test

import (
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	%[1]q
)

const secret = "a-secret-that-is-at-least-thirty-two-bytes"

// secretRequest sends one request through a router guarded by
// RequireSecret(configured) and returns the status.
func secretRequest(configured string, set func(*http.Request)) int {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/guarded", middlewares.RequireSecret(configured), func(c *gin.Context) { c.Status(http.StatusOK) })
	req := httptest.NewRequest(http.MethodGet, "/guarded", nil)
	set(req)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec.Code
}

var _ = Describe("RequireSecret", func() {
	var code int

	Context("with the secret in the header", func() {
		BeforeEach(func() {
			code = secretRequest(secret, func(r *http.Request) { r.Header.Set(middlewares.SecretHeader, secret) })
		})

		It("opens", func() { Expect(code).To(Equal(http.StatusOK)) })
	})

	Context("with the secret as the basic auth password", func() {
		BeforeEach(func() {
			code = secretRequest(secret, func(r *http.Request) { r.SetBasicAuth("anyone", secret) })
		})

		It("opens", func() { Expect(code).To(Equal(http.StatusOK)) })
	})

	Context("with no secret", func() {
		BeforeEach(func() {
			code = secretRequest(secret, func(*http.Request) {})
		})

		It("is refused", func() { Expect(code).To(Equal(http.StatusUnauthorized)) })
	})

	Context("with a wrong secret", func() {
		BeforeEach(func() {
			code = secretRequest(secret, func(r *http.Request) { r.Header.Set(middlewares.SecretHeader, "nope") })
		})

		It("is refused", func() { Expect(code).To(Equal(http.StatusUnauthorized)) })
	})

	Context("with only a prefix of the secret", func() {
		BeforeEach(func() {
			code = secretRequest(secret, func(r *http.Request) { r.Header.Set(middlewares.SecretHeader, secret[:10]) })
		})

		It("is refused", func() { Expect(code).To(Equal(http.StatusUnauthorized)) })
	})

	Context("when no secret is configured", func() {
		BeforeEach(func() {
			code = secretRequest("", func(r *http.Request) { r.Header.Set(middlewares.SecretHeader, "") })
		})

		It("opens nothing", func() { Expect(code).To(Equal(http.StatusUnauthorized)) })
	})
})
`, a.module+"/app/middlewares")},
		{Path: "app/controllers/authentication_secret.go", Content: fmt.Sprintf(`package controllers

import (
	"github.com/gin-gonic/gin"

	%[1]q
)

// WithSecret is the route group a trusted service calls. Every route mounted in
// it needs the shared secret from config (%[3]s in the credentials
// file, %[4]s in the environment) in the %[5]s header. Mount a
// controller here instead of on the router, in routes.go:
//
//	s.Reports.SetupRoutes(s.WithSecret(&r.RouterGroup))
//
// The caller sends one header on every request:
//
//	%[5]s: <the secret>
func (s *Server) WithSecret(r *gin.RouterGroup) *gin.RouterGroup {
	return r.Group("", middlewares.RequireSecret(s.Config.%[7]s))
}
`, a.module+"/app/middlewares", a.name, setting.Path(), setting.Env(), a.header(), strings.ToUpper(a.name), setting.Field())},
	}
	return files, nil, nil
}

func (a auth) token() ([]File, []Wire, error) {
	setting, _ := AuthenticationSetting("token")
	files := []File{
		{Path: "app/services/token/token.go", Content: fmt.Sprintf(`// Package token mints and verifies the bearer tokens a signed-in user
// presents. JWT, HS256, one shared secret: another service can mint
// a token for its user with any JWT library and this service verifies it, so
// users keep living there, or this service mints its own from a sessions
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
// is %[1]s in the credentials file (%[2]s in the environment), at
// least 32 bytes; the issuer is the app's name, so a token minted for
// another service with the same secret is still refused.
type Maker struct {
	Secret []byte
	Issuer string
}

// Claims is what a verified token says.
type Claims struct {
	// Subject is who the token was minted for: the caller's user id, usually.
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
`, setting.Path(), setting.Env())},
		{Path: "app/services/token/token_suite_test.go", Content: suiteFile("token", "Token")},
		{Path: "app/services/token/token_test.go", Content: fmt.Sprintf(`package token_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	%[1]q
)

var maker = token.Maker{Secret: []byte("a-test-secret-of-at-least-thirty-two-bytes"), Issuer: "test"}

var _ = Describe("Maker", func() {
	var (
		raw    string
		claims token.Claims
		err    error
	)

	mint := func(m token.Maker, ttl time.Duration) string {
		signed, err := m.Mint("user-42", ttl)
		Expect(err).NotTo(HaveOccurred())
		return signed
	}

	Describe("Mint and Verify", func() {
		Context("with a valid token", func() {
			BeforeEach(func() {
				raw = mint(maker, time.Minute)
				claims, err = maker.Verify(raw)
			})

			It("verifies", func() { Expect(err).NotTo(HaveOccurred()) })
			It("gives back the subject", func() { Expect(claims.Subject).To(Equal("user-42")) })
			It("carries an id", func() { Expect(claims.ID).NotTo(BeEmpty()) })
			It("expires in the future", func() { Expect(claims.ExpiresAt).To(BeTemporally(">", time.Now())) })
		})

		Context("with an expired token", func() {
			BeforeEach(func() {
				_, err = maker.Verify(mint(maker, -time.Minute))
			})

			It("is refused as expired", func() { Expect(err).To(MatchError(token.ErrExpired)) })
		})

		Context("with a token signed by another secret", func() {
			BeforeEach(func() {
				other := token.Maker{Secret: []byte("another-secret-of-at-least-thirty-two-bytes"), Issuer: "test"}
				_, err = maker.Verify(mint(other, time.Minute))
			})

			It("is refused as invalid", func() { Expect(err).To(MatchError(token.ErrInvalid)) })
		})

		Context("with a token from another issuer", func() {
			BeforeEach(func() {
				other := token.Maker{Secret: maker.Secret, Issuer: "someone-else"}
				_, err = maker.Verify(mint(other, time.Minute))
			})

			It("is refused as invalid", func() { Expect(err).To(MatchError(token.ErrInvalid)) })
		})

		Context("with garbage", func() {
			BeforeEach(func() {
				_, err = maker.Verify("not.a.token")
			})

			It("is refused as invalid", func() { Expect(err).To(MatchError(token.ErrInvalid)) })
		})

		Context("with an empty string", func() {
			BeforeEach(func() {
				_, err = maker.Verify("")
			})

			It("is refused as invalid", func() { Expect(err).To(MatchError(token.ErrInvalid)) })
		})
	})

	Describe("Mint", func() {
		Context("with an empty subject", func() {
			BeforeEach(func() {
				_, err = maker.Mint("", time.Minute)
			})

			It("refuses to mint", func() { Expect(err).To(HaveOccurred()) })
		})
	})
})
`, a.module+"/app/services/token")},
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
// a user id usually, is for the caller and the handler to agree on;
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
		{Path: "app/middlewares/require_user_test.go", Content: fmt.Sprintf(`package middlewares_test

import (
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	%[1]q
	%[2]q
)

var testTokens = token.Maker{Secret: []byte("a-test-secret-of-at-least-thirty-two-bytes"), Issuer: "test"}

func userRequest(authorization string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/me", middlewares.RequireUser(testTokens), func(c *gin.Context) { c.String(http.StatusOK, middlewares.Subject(c)) })
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

var _ = Describe("RequireUser", func() {
	var rec *httptest.ResponseRecorder

	Context("with a good token", func() {
		BeforeEach(func() {
			good, err := testTokens.Mint("user-42", time.Minute)
			Expect(err).NotTo(HaveOccurred())
			rec = userRequest("Bearer " + good)
		})

		It("lets the request through", func() { Expect(rec.Code).To(Equal(http.StatusOK)) })
		It("puts the subject on the context", func() { Expect(rec.Body.String()).To(Equal("user-42")) })
	})

	Context("with no token", func() {
		BeforeEach(func() { rec = userRequest("") })

		It("answers 401", func() { Expect(rec.Code).To(Equal(http.StatusUnauthorized)) })
	})

	Context("with an expired token", func() {
		BeforeEach(func() {
			expired, err := testTokens.Mint("user-42", -time.Minute)
			Expect(err).NotTo(HaveOccurred())
			rec = userRequest("Bearer " + expired)
		})

		It("answers 401", func() { Expect(rec.Code).To(Equal(http.StatusUnauthorized)) })
	})

	Context("with garbage", func() {
		BeforeEach(func() { rec = userRequest("Bearer nope") })

		It("answers 401", func() { Expect(rec.Code).To(Equal(http.StatusUnauthorized)) })
	})

	Context("with the wrong scheme", func() {
		BeforeEach(func() {
			good, err := testTokens.Mint("user-42", time.Minute)
			Expect(err).NotTo(HaveOccurred())
			rec = userRequest("Basic " + good)
		})

		It("answers 401", func() { Expect(rec.Code).To(Equal(http.StatusUnauthorized)) })
	})
})
`, a.module+"/app/middlewares", a.module+"/app/services/token")},
		{Path: "app/controllers/authentication_token.go", Content: fmt.Sprintf(`package controllers

import (
	"github.com/gin-gonic/gin"

	%[1]q
	%[2]q
)

// Tokens is the token maker for this app: the secret from config
// (%[4]s in the credentials file, %[5]s in the environment) and the
// app's name as issuer. A sessions controller that signs users in here mints
// with it; another service that signs users in mints the same token with any JWT
// library: HS256, the same secret, and the claims iss (%[3]q), sub, iat, exp
// and jti.
func (s *Server) Tokens() token.Maker {
	return token.Maker{Secret: []byte(s.Config.%[6]s), Issuer: %[3]q}
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
`, a.module+"/app/middlewares", a.module+"/app/services/token", a.name, setting.Path(), setting.Env(), setting.Field())},
		{Path: "test/testapp/authentication_token.go", Content: `package testapp

import "time"

// As signs a request test in as that token subject: whatever the app's
// sessions mint, usually a user's id as a string. The token is minted with
// the app's own secret, so the real RequireUser accepts it.
//
//	app := testapp.New(GinkgoT())
//	app.Get("/posts").As(strconv.FormatInt(user.ID, 10)).Do().Status(http.StatusOK)
//
// Leave As off to test the 401.
func (r *Request) As(subject string) *Request {
	signed, err := r.app.Server.Tokens().Mint(subject, time.Hour)
	if err != nil {
		r.app.t.Fatal(err)
	}
	r.authorization = "Bearer " + signed
	return r
}
`},
	}
	return files, nil, nil
}

// fakeIDAssign is a Go expression the api_keys service's fake store uses to
// make up an id for a key it "creates", in the domain's id type: a string
// derived from the prefix under the uuid scheme (any string compiles,
// whether or not it looks like a uuid), or a counter under bigint.
func fakeIDAssign(scheme IDScheme) string {
	if scheme == IDUUID {
		return `"id-" + k.Prefix`
	}
	return "int64(len(f.keys) + 1)"
}

func (a auth) apiKey(now time.Time) ([]File, []Wire, error) {
	attrs, err := ParseAttrs([]string{"name:string", "prefix:string:uniq", "digest:string"})
	if err != nil {
		return nil, nil, err
	}
	files, err := Model(a.module, "api_key", attrs, a.scheme, now)
	if err != nil {
		return nil, nil, err
	}
	// One query the model generator does not write: the lookup a presented
	// key is checked with, by its public prefix.
	for i := range files {
		switch files[i].Path {
		case "db/queries/api_keys.sql":
			files[i].Content += "\n-- name: GetApiKeyByPrefix :one\nSELECT * FROM api_keys WHERE prefix = $1;\n"
		case "db/adapter/api_keys.go":
			files[i].Content += `
// GetApiKeyByPrefix finds the key a presented "<prefix>.<secret>" names, by
// its prefix; the service compares the secret against the digest.
func (s *Store) GetApiKeyByPrefix(ctx context.Context, prefix string) (models.ApiKey, error) {
	row, err := s.q.GetApiKeyByPrefix(ctx, prefix)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return models.ApiKey{}, models.ErrNotFound
	case err != nil:
		return models.ApiKey{}, fmt.Errorf("store: get api key by prefix: %w", err)
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
	CreateApiKey(ctx context.Context, k models.ApiKey) (models.ApiKey, error)
	GetApiKeyByPrefix(ctx context.Context, prefix string) (models.ApiKey, error)
}

// Service makes and verifies keys.
type Service struct {
	Store store
}

// New builds the service.
func New(s store) *Service { return &Service{Store: s} }

// Create makes a key for name and returns it in full, "<prefix>.<secret>",
// the only time the secret is ever seen; and the row that was stored.
func (s *Service) Create(ctx context.Context, name string) (string, models.ApiKey, error) {
	prefix, err := random(6)
	if err != nil {
		return "", models.ApiKey{}, err
	}
	secret, err := random(24)
	if err != nil {
		return "", models.ApiKey{}, err
	}
	digest, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
	if err != nil {
		return "", models.ApiKey{}, fmt.Errorf("api keys: digest: %%w", err)
	}
	key, err := s.Store.CreateApiKey(ctx, models.ApiKey{Name: name, Prefix: prefix, Digest: string(digest)})
	if err != nil {
		return "", models.ApiKey{}, err
	}
	return prefix + "." + secret, key, nil
}

// Verify checks a presented key and returns the row it names.
func (s *Service) Verify(ctx context.Context, presented string) (models.ApiKey, error) {
	prefix, secret, ok := strings.Cut(presented, ".")
	if !ok || prefix == "" || secret == "" {
		return models.ApiKey{}, ErrInvalid
	}
	key, err := s.Store.GetApiKeyByPrefix(ctx, prefix)
	if err != nil {
		return models.ApiKey{}, ErrInvalid
	}
	if bcrypt.CompareHashAndPassword([]byte(key.Digest), []byte(secret)) != nil {
		return models.ApiKey{}, ErrInvalid
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
`, a.module+"/app/models")},
		File{Path: "app/services/api_keys/api_keys_suite_test.go", Content: suiteFile("api_keys", "APIKeys")},
		File{Path: "app/services/api_keys/api_keys_test.go", Content: fmt.Sprintf(`package api_keys_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	%[1]q
	%[3]q
)

// fakeStore keeps keys in memory, by prefix.
type fakeStore struct{ keys map[string]models.ApiKey }

func (f *fakeStore) CreateApiKey(_ context.Context, k models.ApiKey) (models.ApiKey, error) {
	k.ID = %[2]s
	f.keys[k.Prefix] = k
	return k, nil
}

func (f *fakeStore) GetApiKeyByPrefix(_ context.Context, prefix string) (models.ApiKey, error) {
	k, ok := f.keys[prefix]
	if !ok {
		return models.ApiKey{}, models.ErrNotFound
	}
	return k, nil
}

var _ = Describe("Service", func() {
	var (
		svc       *api_keys.Service
		ctx       context.Context
		presented string
		made      models.ApiKey
	)

	BeforeEach(func() {
		svc = api_keys.New(&fakeStore{keys: map[string]models.ApiKey{}})
		ctx = context.Background()
	})

	Describe("Create", func() {
		Context("with a name", func() {
			BeforeEach(func() {
				var err error
				presented, made, err = svc.Create(ctx, "billing")
				Expect(err).NotTo(HaveOccurred())
			})

			It("stores the name", func() { Expect(made.Name).To(Equal("billing")) })
			It("stores a digest, never the key", func() {
				Expect(made.Digest).NotTo(BeEmpty())
				Expect(made.Digest).NotTo(Equal(presented))
			})
		})
	})

	Describe("Verify", func() {
		Context("with a key the service made", func() {
			BeforeEach(func() {
				var err error
				presented, made, err = svc.Create(ctx, "billing")
				Expect(err).NotTo(HaveOccurred())
			})

			It("returns the row it names", func() {
				got, err := svc.Verify(ctx, presented)
				Expect(err).NotTo(HaveOccurred())
				Expect(got.ID).To(Equal(made.ID))
			})

			Context("with a wrong secret", func() {
				It("is refused", func() {
					_, err := svc.Verify(ctx, made.Prefix+".nope")
					Expect(err).To(MatchError(api_keys.ErrInvalid))
				})
			})

			Context("with a wrong prefix", func() {
				It("is refused", func() {
					_, err := svc.Verify(ctx, "nope."+presented[len(made.Prefix)+1:])
					Expect(err).To(MatchError(api_keys.ErrInvalid))
				})
			})
		})

		Context("with a key that has no dot", func() {
			It("is refused", func() {
				_, err := svc.Verify(ctx, "nope")
				Expect(err).To(MatchError(api_keys.ErrInvalid))
			})
		})

		Context("with an empty key", func() {
			It("is refused", func() {
				_, err := svc.Verify(ctx, "")
				Expect(err).To(MatchError(api_keys.ErrInvalid))
			})
		})
	})
})
`, a.module+"/app/models", fakeIDAssign(a.scheme), a.module+"/app/services/api_keys")},
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
	Verify(ctx context.Context, presented string) (models.ApiKey, error)
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
`, a.module+"/app/models", a.module+"/app/views", a.name, a.name)},
		File{Path: "app/middlewares/require_api_key_test.go", Content: fmt.Sprintf(`package middlewares_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	%[1]q
	%[2]q
)

// fakeAPIKeys accepts one key, "good.key".
type fakeAPIKeys struct{}

func (fakeAPIKeys) Verify(_ context.Context, presented string) (models.ApiKey, error) {
	if presented == "good.key" {
		return models.ApiKey{Name: "billing"}, nil
	}
	return models.ApiKey{}, errors.New("invalid")
}

func apiKeyRequest(authorization string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/guarded", middlewares.RequireAPIKey(fakeAPIKeys{}), func(c *gin.Context) { c.String(http.StatusOK, middlewares.APIKeyName(c)) })
	req := httptest.NewRequest(http.MethodGet, "/guarded", nil)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

var _ = Describe("RequireAPIKey", func() {
	var rec *httptest.ResponseRecorder

	Context("with a good key", func() {
		BeforeEach(func() { rec = apiKeyRequest("Bearer good.key") })

		It("lets the request through", func() { Expect(rec.Code).To(Equal(http.StatusOK)) })
		It("puts the key's name on the context", func() { Expect(rec.Body.String()).To(Equal("billing")) })
	})

	Context("with no key", func() {
		BeforeEach(func() { rec = apiKeyRequest("") })

		It("answers 401", func() { Expect(rec.Code).To(Equal(http.StatusUnauthorized)) })
	})

	Context("with a wrong key", func() {
		BeforeEach(func() { rec = apiKeyRequest("Bearer bad.key") })

		It("answers 401", func() { Expect(rec.Code).To(Equal(http.StatusUnauthorized)) })
	})

	Context("with the wrong scheme", func() {
		BeforeEach(func() { rec = apiKeyRequest("Basic good.key") })

		It("answers 401", func() { Expect(rec.Code).To(Equal(http.StatusUnauthorized)) })
	})
})
`, a.module+"/app/middlewares", a.module+"/app/models")},
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
Through the tool: bogie api_keys:create NAME
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
	store, err := adapter.Open(ctx, cfg.Database.URL)
	if err != nil {
		return err
	}
	defer store.Close()
	key, made, err := api_keys.New(store).Create(ctx, args[1])
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "api key %%q made (%%v); it is shown once:\n", made.Name, made.ID)
	fmt.Println(key)
	return nil
}
`, a.module+"/db/adapter", a.module+"/app/services/api_keys", a.module+"/config", a.name)},
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

// AuthenticationFiles are the paths a shape writes, for destroy. scheme
// does not change which paths are written, only their content, so destroy
// can pass the app's recorded scheme without needing to get it exactly
// right.
func AuthenticationFiles(module, name, shape string, scheme IDScheme) ([]File, error) {
	files, _, err := Authentication(module, name, shape, scheme, time.Time{})
	return files, err
}
