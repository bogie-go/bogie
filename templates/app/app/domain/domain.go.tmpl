// Package domain is the application's own vocabulary: the types and rules that
// would still be true if HTTP, Postgres and Gin all disappeared.
//
// It imports nothing third-party. If holding that becomes hard, the boundary
// is in the wrong place and you will know immediately. Ids are strings here
// and become uuid at the store boundary; that conversion living in app/models
// is what lets this package stay dependency-free.
//
// `bogie g model` adds a type and its validations here, one file per model.
// The two errors below are what every model's layers above map to HTTP
// codes; they live here, not with any one model.
package domain

import "errors"

// ErrNotFound is returned for an id that names nothing. An id that is not
// even a uuid gets the same answer: it cannot exist, and a caller should not
// be able to tell the two apart.
var ErrNotFound = errors.New("not found")

// ErrInvalid wraps every validation failure, so a controller can answer 422
// with errors.Is and never has to know the individual rules.
var ErrInvalid = errors.New("invalid")
