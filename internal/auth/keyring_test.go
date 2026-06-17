package auth

import (
	"errors"
	"testing"

	"github.com/zalando/go-keyring"
)

// fakeKeyring is an in-memory keyring used to exercise the keyring code paths
// deterministically, without touching the real OS keychain.
type fakeKeyring struct {
	store   map[string]string
	failSet bool
	failGet error
}

func newFakeKeyring() *fakeKeyring {
	return &fakeKeyring{store: map[string]string{}}
}

// install wires the fake into the package seams and restores them on cleanup.
func (f *fakeKeyring) install(t *testing.T) {
	t.Helper()
	origSet, origGet, origDel := keyringSet, keyringGet, keyringDelete
	t.Cleanup(func() {
		keyringSet, keyringGet, keyringDelete = origSet, origGet, origDel
	})
	keyringSet = func(_, key, value string) error {
		// The availability probe must still succeed so the keyring path is
		// taken; only real writes honor failSet.
		if f.failSet && key != "__lk_probe__" {
			return errors.New("set failed")
		}
		f.store[key] = value
		return nil
	}
	keyringGet = func(_, key string) (string, error) {
		if f.failGet != nil {
			return "", f.failGet
		}
		v, ok := f.store[key]
		if !ok {
			return "", keyring.ErrNotFound
		}
		return v, nil
	}
	keyringDelete = func(_, key string) error {
		if _, ok := f.store[key]; !ok {
			return keyring.ErrNotFound
		}
		delete(f.store, key)
		return nil
	}
}

// keyringOn enables the keyring path (clears LK_NO_KEYRING and LK_TOKEN).
func keyringOn(t *testing.T) {
	t.Helper()
	t.Setenv(EnvNoKeyring, "")
	t.Setenv(EnvToken, "")
}

func TestKeyringRoundTrip(t *testing.T) {
	keyringOn(t)
	newFakeKeyring().install(t)

	if err := Save(testOrigin, "lkn_abc_def"); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	tok, src, err := Load(testOrigin)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if tok != "lkn_abc_def" || src != SourceKeyring {
		t.Errorf("got token=%q src=%q", tok, src)
	}
	if err := Delete(testOrigin); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}
	tok, src, err = Load(testOrigin)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if tok != "" || src != SourceNone {
		t.Errorf("after delete token=%q src=%q", tok, src)
	}
}

func TestKeyringDeleteMissingIsNoError(t *testing.T) {
	keyringOn(t)
	newFakeKeyring().install(t)
	if err := Delete(testOrigin); err != nil {
		t.Errorf("Delete() on missing keyring entry should be nil, got %v", err)
	}
}

func TestKeyringSaveError(t *testing.T) {
	keyringOn(t)
	f := newFakeKeyring()
	f.failSet = true
	f.install(t)
	if err := Save(testOrigin, "lkn_abc_def"); err == nil {
		t.Error("expected Save error when keyring set fails")
	}
}

func TestKeyringLoadError(t *testing.T) {
	keyringOn(t)
	f := newFakeKeyring()
	f.failGet = errors.New("boom")
	f.install(t)
	if _, _, err := Load(testOrigin); err == nil {
		t.Error("expected Load error when keyring get fails")
	}
}

func TestKeyringDeleteError(t *testing.T) {
	keyringOn(t)
	newFakeKeyring().install(t)
	origDel := keyringDelete
	defer func() { keyringDelete = origDel }()
	keyringDelete = func(_, key string) error {
		if key == "__lk_probe__" {
			return nil
		}
		return errors.New("delete boom")
	}
	if err := Delete(testOrigin); err == nil {
		t.Error("expected Delete error when keyring delete fails")
	}
}

func TestKeyringAvailableProbeFails(t *testing.T) {
	keyringOn(t)
	origSet := keyringSet
	defer func() { keyringSet = origSet }()
	keyringSet = func(_, _, _ string) error { return errors.New("no keyring") }
	if keyringAvailable() {
		t.Error("keyringAvailable() should be false when probe write fails")
	}
}

func TestKeyringAvailableProbeSucceeds(t *testing.T) {
	keyringOn(t)
	newFakeKeyring().install(t)
	if !keyringAvailable() {
		t.Error("keyringAvailable() should be true when probe succeeds")
	}
}
