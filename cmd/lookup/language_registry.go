package main

import (
	"fmt"

	"github.com/poizdev/lookup/internal/language"
	"github.com/poizdev/lookup/internal/language/csharp"
	"github.com/poizdev/lookup/internal/language/golang"
	"github.com/poizdev/lookup/internal/language/python"
	"github.com/poizdev/lookup/internal/language/rust"
)

func newLanguageRegistry() (*language.Registry, error) {
	registry := language.NewRegistry()
	for _, adapter := range []language.LanguageAdapter{
		&golang.Adapter{}, &python.Adapter{}, &rust.Adapter{}, &csharp.Adapter{},
	} {
		if err := registry.Register(adapter); err != nil {
			return nil, fmt.Errorf("register %s language adapter: %w", adapter.Language(), err)
		}
	}
	return registry, nil
}
