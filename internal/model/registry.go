package model

import (
	"context"
	"errors"
	"fmt"
	"sync"

	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	einomodel "github.com/cloudwego/eino/components/model"
)

// ErrUnknownModel is returned for a model id that is not in the allow-list.
var ErrUnknownModel = errors.New("unknown model")

// Factory builds the chat model for one model id.
type Factory func(ctx context.Context, id string) (einomodel.BaseChatModel, error)

// Registry serves chat models by id. Every model shares one OpenAI-compatible
// endpoint and API key (DashScope); only the model name differs. Instances are
// created lazily on first use and cached.
type Registry struct {
	infos     []Info
	allowed   map[string]struct{}
	defaultID string
	factory   Factory

	mu    sync.Mutex
	cache map[string]einomodel.BaseChatModel
}

// NewRegistry builds a Registry whose models talk to baseURL with apiKey.
func NewRegistry(baseURL, apiKey string, ids []string, defaultID string) (*Registry, error) {
	return NewRegistryWithFactory(ids, defaultID, func(ctx context.Context, id string) (einomodel.BaseChatModel, error) {
		m, err := einoopenai.NewChatModel(ctx, &einoopenai.ChatModelConfig{
			BaseURL: baseURL,
			APIKey:  apiKey,
			Model:   id,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create chat model %q: %w", id, err)
		}
		return m, nil
	})
}

// NewRegistryWithFactory is NewRegistry with a custom model factory (used by tests).
// ids is the ordered allow-list; defaultID must be one of them.
func NewRegistryWithFactory(ids []string, defaultID string, factory Factory) (*Registry, error) {
	r := &Registry{
		allowed:   make(map[string]struct{}, len(ids)),
		defaultID: defaultID,
		factory:   factory,
		cache:     make(map[string]einomodel.BaseChatModel),
	}
	for _, id := range ids {
		if _, dup := r.allowed[id]; dup || id == "" {
			continue
		}
		r.allowed[id] = struct{}{}
		r.infos = append(r.infos, Describe(id))
	}
	if len(r.infos) == 0 {
		return nil, errors.New("model registry: no models configured")
	}
	if _, ok := r.allowed[defaultID]; !ok {
		return nil, fmt.Errorf("model registry: default model %q is not in the allow-list", defaultID)
	}
	return r, nil
}

// Models lists the selectable models in configured order.
func (r *Registry) Models() []Info {
	out := make([]Info, len(r.infos))
	copy(out, r.infos)
	return out
}

// Default is the model used when a request does not choose one.
func (r *Registry) Default() string { return r.defaultID }

// Resolve maps a requested id to a usable one: empty means the default, and an
// id outside the allow-list yields ErrUnknownModel.
func (r *Registry) Resolve(id string) (string, error) {
	if id == "" {
		return r.defaultID, nil
	}
	if _, ok := r.allowed[id]; !ok {
		return "", fmt.Errorf("%w: %q", ErrUnknownModel, id)
	}
	return id, nil
}

// Get returns the chat model for id (empty = default), creating and caching it
// on first use. A failed creation is not cached, so the next call retries.
func (r *Registry) Get(ctx context.Context, id string) (einomodel.BaseChatModel, error) {
	id, err := r.Resolve(id)
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if m, ok := r.cache[id]; ok {
		return m, nil
	}
	m, err := r.factory(ctx, id)
	if err != nil {
		return nil, err
	}
	r.cache[id] = m
	return m, nil
}
