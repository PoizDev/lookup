// Package apperror defines UI-independent error categories for CLI boundaries.
package apperror

import "errors"

type Kind string

const (
	KindAuthentication         Kind = "authentication"
	KindRateLimit              Kind = "rate_limit"
	KindUnavailable            Kind = "provider_unavailable"
	KindMalformedResponse      Kind = "malformed_response"
	KindInvalidRequest         Kind = "invalid_request"
	KindNetworkTimeout         Kind = "network_timeout"
	KindConfiguration          Kind = "configuration"
	KindFilesystemPermission   Kind = "filesystem_permission"
	KindPathNotFound           Kind = "path_not_found"
	KindProviderInitialization Kind = "provider_initialization"
	// KindContextInsufficient is returned by a provider when the request cannot
	// fit within the effective context window even after bounded context minimization.
	// It is a local preflight rejection — no HTTP request is ever sent — and must
	// not be counted as a provider transport or schema failure.
	KindContextInsufficient Kind = "context_insufficient"
)

type Error struct {
	Kind    Kind
	Message string
	Cause   error
}

func New(kind Kind, message string) error { return &Error{Kind: kind, Message: message} }
func Wrap(kind Kind, message string, cause error) error {
	return &Error{Kind: kind, Message: message, Cause: cause}
}
func (e *Error) Error() string {
	if e.Cause == nil {
		return e.Message
	}
	if e.Message == "" {
		return e.Cause.Error()
	}
	return e.Message + ": " + e.Cause.Error()
}
func (e *Error) Unwrap() error { return e.Cause }
func IsKind(err error, kind Kind) bool {
	var target *Error
	return errors.As(err, &target) && target.Kind == kind
}
