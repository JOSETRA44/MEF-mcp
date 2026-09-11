package core

import (
	"context"
	"errors"
	"fmt"
	"net"
)

// ErrorCode is the machine-readable failure class from ARSENAL-SPEC §1.
type ErrorCode string

const (
	CodeUsage     ErrorCode = "usage"
	CodeNotFound  ErrorCode = "not_found"
	CodeAuth      ErrorCode = "auth"
	CodeRateLimit ErrorCode = "rate_limit"
	CodeNetwork   ErrorCode = "network"
	CodeAPI       ErrorCode = "api"
	CodeInternal  ErrorCode = "internal"
)

// ExitCodes documents every process exit status (ARSENAL-SPEC §2). The
// describe manifest publishes this table verbatim.
var ExitCodes = map[string]string{
	"0": "ok (an empty result set is still success)",
	"1": "api: the MEF source returned an error or an unexpected response",
	"2": "usage: bad flag, argument, dimension or column name",
	"3": "not_found: the resource, CUI or dataset does not exist",
	"4": "auth: n/a - every MEF source used here is public, no credentials",
	"5": "rate_limit: the MEF source throttled us (HTTP 429/503)",
	"6": "network: timeout, DNS or connection failure",
}

var exitByCode = map[ErrorCode]int{
	CodeAPI:       1,
	CodeInternal:  1,
	CodeUsage:     2,
	CodeNotFound:  3,
	CodeAuth:      4,
	CodeRateLimit: 5,
	CodeNetwork:   6,
}

// defaultHints guarantees every error carries a next step, even when the call
// site forgot one. An error path that cannot explain itself is not actionable.
var defaultHints = map[ErrorCode]string{
	CodeUsage:     "Run `mef describe <command> --json` to see valid arguments.",
	CodeNotFound:  "Check the identifier; list candidates with the matching search command.",
	CodeAuth:      "No credentials are needed; report this as a bug.",
	CodeRateLimit: "Wait a minute and retry; cached results are served with no request.",
	CodeNetwork:   "Check connectivity; run `mef doctor` to see which MEF source is down.",
	CodeAPI:       "The MEF source misbehaved; retry later or run `mef doctor`.",
	CodeInternal:  "This is a bug in mef; please report it with the command you ran.",
}

// Error is the one error type commands return. Everything else is wrapped into
// it by AsError before it reaches the CLI or the MCP server.
type Error struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
	Hint    string    `json:"hint"`
	cause   error
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.cause }

// ExitCode maps the error class to the process exit status.
func (e *Error) ExitCode() int {
	if c, ok := exitByCode[e.Code]; ok {
		return c
	}
	return 1
}

// NewError builds an Error; an empty hint is replaced by the class default.
func NewError(code ErrorCode, hint, format string, args ...any) *Error {
	if hint == "" {
		hint = defaultHints[code]
	}
	return &Error{Code: code, Message: fmt.Sprintf(format, args...), Hint: hint}
}

// Wrap attaches a cause while keeping the classified message.
func Wrap(code ErrorCode, cause error, hint, format string, args ...any) *Error {
	e := NewError(code, hint, format, args...)
	e.cause = cause
	return e
}

// Usagef is shorthand for the most common error a caller can fix themselves.
func Usagef(hint, format string, args ...any) *Error {
	return NewError(CodeUsage, hint, format, args...)
}

// NotFoundf is shorthand for a missing resource.
func NotFoundf(hint, format string, args ...any) *Error {
	return NewError(CodeNotFound, hint, format, args...)
}

// AsError classifies any error. Context deadlines and net errors become
// network failures; anything unrecognised is internal.
func AsError(err error) *Error {
	if err == nil {
		return nil
	}
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return Wrap(CodeNetwork, err, "", "timed out waiting for the MEF source")
	}
	if errors.Is(err, context.Canceled) {
		return Wrap(CodeNetwork, err, "The request was cancelled.", "request cancelled")
	}
	var ne net.Error
	if errors.As(err, &ne) {
		return Wrap(CodeNetwork, err, "", "network error: %v", err)
	}
	return Wrap(CodeInternal, err, "", "%v", err)
}
