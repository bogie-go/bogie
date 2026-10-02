// Package views shapes what goes out over HTTP, and what comes in.
//
// It earns its place for a reason beyond tidiness: these are published
// contracts, and a domain struct doubling as a wire struct means renaming a
// field silently changes an API. Nothing in here imports the domain; the
// controller converts.
package views

// Response is the envelope every JSON reply without a body of its own uses.
type Response struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// OK is a bare success.
func OK() Response { return Response{Status: "ok"} }

// Error wraps a failure. The message is for an operator reading logs, not for
// a customer; choose the words with ErrorMessage when they cross a boundary.
func Error(err error) Response {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	return Response{Status: "error", Error: msg}
}

// ErrorMessage is an error response with wording chosen by the caller.
func ErrorMessage(msg string) Response {
	return Response{Status: "error", Error: msg}
}
