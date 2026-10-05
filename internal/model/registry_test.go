package model

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type fakeModel struct{ id string }

func (fakeModel) Generate(context.Context, []*schema.Message, ...einomodel.Option) (*schema.Message, error) {
	return schema.AssistantMessage("ok", nil), nil
}

func (fakeModel) Stream(context.Context, []*schema.Message, ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("not used")
}

func countingFactory(calls *sync.Map) Factory {
	return func(_ context.Context, id string) (einomodel.BaseChatModel, error) {
		c, _ := calls.LoadOrStore(id, new(atomic.Int32))
		c.(*atomic.Int32).Add(1)
		return fakeModel{id: id}, nil
	}
}

func TestRegistry_AllowListAndDefault(t *testing.T) {
	if _, err := NewRegistryWithFactory(nil, "a", nil); err == nil {
		t.Error("an empty allow-list must be rejected")
	}
	if _, err := NewRegistryWithFactory([]string{"a", "b"}, "c", nil); err == nil {
		t.Error("a default outside the allow-list must be rejected")
	}

	r, err := NewRegistryWithFactory([]string{"b", "a", "b", "", "c"}, "a", nil)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range r.Models() {
		ids = append(ids, m.ID)
	}
	if want := []string{"b", "a", "c"}; len(ids) != 3 || ids[0] != want[0] || ids[1] != want[1] || ids[2] != want[2] {
		t.Errorf("Models() = %v, want %v (configured order, duplicates and blanks dropped)", ids, want)
	}
	if r.Default() != "a" {
		t.Errorf("Default() = %q", r.Default())
	}

	// Models() must hand out a copy.
	r.Models()[0].Label = "tampered"
	if r.Models()[0].Label == "tampered" {
		t.Error("Models() exposes internal state")
	}
}

func TestRegistry_Resolve(t *testing.T) {
	r, _ := NewRegistryWithFactory([]string{"a", "b"}, "a", nil)
	if id, err := r.Resolve(""); err != nil || id != "a" {
		t.Errorf("empty -> %q, %v; want the default", id, err)
	}
	if id, err := r.Resolve("b"); err != nil || id != "b" {
		t.Errorf("b -> %q, %v", id, err)
	}
	for _, bad := range []string{"B", "c", " a", "a ", "../a"} {
		if _, err := r.Resolve(bad); !errors.Is(err, ErrUnknownModel) {
			t.Errorf("Resolve(%q) = %v, want ErrUnknownModel", bad, err)
		}
	}
}

func TestRegistry_GetCachesPerModelUnderConcurrency(t *testing.T) {
	var calls sync.Map
	r, _ := NewRegistryWithFactory([]string{"a", "b", "c"}, "a", countingFactory(&calls))

	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := []string{"a", "b", "c", ""}[i%4] // "" resolves to "a"
			if _, err := r.Get(context.Background(), id); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()

	for _, id := range []string{"a", "b", "c"} {
		c, ok := calls.Load(id)
		if !ok {
			t.Fatalf("model %s was never created", id)
		}
		if n := c.(*atomic.Int32).Load(); n != 1 {
			t.Errorf("model %s created %d times, want exactly 1", id, n)
		}
	}
}

func TestRegistry_GetIsLazy(t *testing.T) {
	var calls sync.Map
	r, _ := NewRegistryWithFactory([]string{"a", "b"}, "a", countingFactory(&calls))
	if _, err := r.Get(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
	if _, ok := calls.Load("b"); ok {
		t.Error("an unused model must not be created")
	}
}

func TestRegistry_GetRejectsUnknownWithoutCallingFactory(t *testing.T) {
	var calls sync.Map
	r, _ := NewRegistryWithFactory([]string{"a"}, "a", countingFactory(&calls))
	if _, err := r.Get(context.Background(), "nope"); !errors.Is(err, ErrUnknownModel) {
		t.Fatalf("got %v, want ErrUnknownModel", err)
	}
	if _, ok := calls.Load("nope"); ok {
		t.Error("factory was called for a model outside the allow-list")
	}
}

func TestRegistry_FailedCreationIsNotCached(t *testing.T) {
	var attempts atomic.Int32
	r, _ := NewRegistryWithFactory([]string{"a"}, "a", func(context.Context, string) (einomodel.BaseChatModel, error) {
		if attempts.Add(1) == 1 {
			return nil, errors.New("transient")
		}
		return fakeModel{id: "a"}, nil
	})
	if _, err := r.Get(context.Background(), "a"); err == nil {
		t.Fatal("first call should fail")
	}
	if _, err := r.Get(context.Background(), "a"); err != nil {
		t.Fatalf("second call should retry and succeed, got %v", err)
	}
}

func TestCatalog(t *testing.T) {
	ids := DefaultIDs()
	if len(ids) != 12 || ids[0] != DefaultModelID {
		t.Fatalf("DefaultIDs() = %v", ids)
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Errorf("duplicate id %s", id)
		}
		seen[id] = true
		info := Describe(id)
		if info.Label == "" {
			t.Errorf("%s has no label", id)
		}
		if info.Tier != TierFlagship && info.Tier != TierStandard && info.Tier != TierFast {
			t.Errorf("%s has unknown tier %q", id, info.Tier)
		}
	}

	if got := Describe("qwen3.8-27b"); got.Tier != TierStandard || got.Label != "通义千问 3.8 27B" {
		t.Errorf("Describe(known) = %+v", got)
	}
	// Unknown ids stay usable: label = id, tier = standard.
	if got := Describe("brand-new-model"); got.ID != "brand-new-model" || got.Label != "brand-new-model" || got.Tier != TierStandard {
		t.Errorf("Describe(unknown) = %+v", got)
	}
}
