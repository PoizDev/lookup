// Package semantic defines language-independent facts consumed by deterministic rules.
package semantic

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

type FactKind string

const (
	FactSource               FactKind = "source"
	FactSink                 FactKind = "sink"
	FactSanitizer            FactKind = "sanitizer"
	FactGuard                FactKind = "guard"
	FactRoute                FactKind = "route"
	FactMiddleware           FactKind = "middleware"
	FactDatabaseOperation    FactKind = "database_operation"
	FactHTTPInput            FactKind = "http_input"
	FactCommandExecution     FactKind = "command_execution"
	FactFileOperation        FactKind = "file_operation"
	FactCryptoOperation      FactKind = "crypto_operation"
	FactResourceAcquire      FactKind = "resource_acquire"
	FactResourceRelease      FactKind = "resource_release"
	FactConcurrencyOperation FactKind = "concurrency_operation"
	FactPropagation          FactKind = "propagation"
	FactCall                 FactKind = "call"
	FactReturn               FactKind = "return"
	FactControl              FactKind = "control"
)

type Location struct {
	File        string `json:"file"`
	StartLine   int    `json:"start_line"`
	StartColumn int    `json:"start_column,omitempty"`
	EndLine     int    `json:"end_line,omitempty"`
	EndColumn   int    `json:"end_column,omitempty"`
}

type Fact struct {
	ID             string            `json:"id"`
	Kind           FactKind          `json:"kind"`
	Operation      string            `json:"operation"`
	Location       Location          `json:"location"`
	Function       string            `json:"function,omitempty"`
	Receiver       string            `json:"receiver,omitempty"`
	Expression     string            `json:"expression,omitempty"`
	Arguments      []string          `json:"arguments,omitempty"`
	ArgumentInputs [][]string        `json:"argument_inputs,omitempty"`
	Inputs         []string          `json:"inputs,omitempty"`
	Outputs        []string          `json:"outputs,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

func NewFact(kind FactKind, operation string, location Location) Fact {
	key := fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%d", kind, operation, location.File, location.StartLine, location.StartColumn)
	digest := sha256.Sum256([]byte(key))
	return Fact{ID: hex.EncodeToString(digest[:12]), Kind: kind, Operation: operation, Location: location}
}
