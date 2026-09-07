package language

import (
	"fmt"
	"reflect"
	"sort"
	"sync"
)

// Registry holds registered language adapters.
type Registry struct {
	mu       sync.RWMutex
	adapters map[Language]LanguageAdapter
}

// NewRegistry creates and returns a new Registry instance.
func NewRegistry() *Registry {
	return &Registry{
		adapters: make(map[Language]LanguageAdapter),
	}
}

// Register adds a language adapter to the registry.
func (r *Registry) Register(adapter LanguageAdapter) error {
	if adapter == nil || isNilAdapter(adapter) {
		return fmt.Errorf("register language adapter: adapter is nil")
	}
	if adapter.Language() == "" {
		return fmt.Errorf("register language adapter: language is empty")
	}
	if err := adapter.Capabilities().Validate(); err != nil {
		return fmt.Errorf("register %s adapter: %w", adapter.Language(), err)
	}
	if adapter.Capabilities().Semantic {
		if _, ok := adapter.(SemanticAdapter); !ok {
			return fmt.Errorf("register %s adapter: semantic capability requires SemanticAdapter", adapter.Language())
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.adapters[adapter.Language()]; exists {
		return fmt.Errorf("register %s adapter: already registered", adapter.Language())
	}
	r.adapters[adapter.Language()] = adapter
	return nil
}

// Adapters returns a stable snapshot sorted by language ID.
func (r *Registry) Adapters() []LanguageAdapter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	adapters := make([]LanguageAdapter, 0, len(r.adapters))
	for _, adapter := range r.adapters {
		adapters = append(adapters, adapter)
	}
	sort.Slice(adapters, func(i, j int) bool { return adapters[i].Language() < adapters[j].Language() })
	return adapters
}

func isNilAdapter(adapter LanguageAdapter) bool {
	value := reflect.ValueOf(adapter)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

// Get returns the adapter for a language, or nil if not registered.
func (r *Registry) Get(lang Language) LanguageAdapter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.adapters[lang]
}

// GetByName returns the adapter for a language name string.
func (r *Registry) GetByName(name string) LanguageAdapter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.adapters[Language(name)]
}

// Languages returns all registered languages.
func (r *Registry) Languages() []Language {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var langs []Language
	for lang := range r.adapters {
		langs = append(langs, lang)
	}
	sort.Slice(langs, func(i, j int) bool { return langs[i] < langs[j] })
	return langs
}

// Has checks if an adapter exists for a language.
func (r *Registry) Has(lang Language) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.adapters[lang]
	return exists
}
