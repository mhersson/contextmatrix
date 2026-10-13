package jira

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
)

// Authorizer sets credentials on an outgoing request.
type Authorizer interface {
	Apply(req *http.Request) error
}

// headerAuth sets a precomputed Authorization header. It is only handed out as
// a pointer: fmt does not call String on a value held in an unexported field,
// so a Client holding a value would print the header. The String methods keep
// the secret out of %v, %+v and %#v so an accidental log line cannot leak it.
type headerAuth struct {
	mode   string
	header string
}

func (a headerAuth) Apply(req *http.Request) error {
	req.Header.Set("Authorization", a.header)

	return nil
}

func (a headerAuth) String() string { return "jira " + a.mode + " authorizer" }

func (a headerAuth) GoString() string { return a.String() }

// NewAuthorizer builds the Authorizer for a jira.auth_mode: "basic" sends
// email:token (personal or bot API token), "bearer" sends the token alone
// (service-account key).
func NewAuthorizer(mode, email, token string) (Authorizer, error) {
	switch mode {
	case "basic":
		if email == "" || token == "" {
			return nil, errors.New("jira auth: basic mode needs email and token")
		}

		creds := base64.StdEncoding.EncodeToString([]byte(email + ":" + token))

		return &headerAuth{mode: mode, header: "Basic " + creds}, nil
	case "bearer":
		if token == "" {
			return nil, errors.New("jira auth: bearer mode needs a token")
		}

		return &headerAuth{mode: mode, header: "Bearer " + token}, nil
	default:
		return nil, fmt.Errorf("jira auth: unknown auth_mode %q (want basic or bearer)", mode)
	}
}
