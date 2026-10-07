package jeff

import (
	"errors"
	"fmt"
)

// ErrNoToken is returned by NewClient when WithRequireAuth was set and no
// bearer token is configured (neither WithToken nor JEFF_TOKEN env). It
// fails construction instead of building a client that 401s on every call.
var ErrNoToken = errors.New("jeff: auth required but no token configured")

// ErrNoAnswer is returned by Ask/AskNoul when the response carries no
// answer under the requested question name — the server accepted the
// request but produced nothing for it.
var ErrNoAnswer = errors.New("jeff: response missing requested answer")

// StatusError is a non-2xx response from the service.
type StatusError struct {
	StatusCode int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("jeff: server returned %d", e.StatusCode)
}
