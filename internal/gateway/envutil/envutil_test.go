package envutil

import (
	"errors"
	"testing"
)

func TestMergeEnvMaskedSecretRequiresExistingValue(t *testing.T) {
	base := map[string]string{}
	set := map[string]string{"SECRET_KEY": MaskedSecret}

	_, _, err := MergeEnv(base, set, nil, MergeOptions{
		AllowSecretUpdates: true,
		IsSecretKey: func(key string) bool {
			return key == "SECRET_KEY"
		},
	})

	if err == nil || err != ErrMaskedSecretWithoutBase {
		t.Fatalf("expected ErrMaskedSecretWithoutBase, got %v", err)
	}
}

func TestMergeEnvMaskedSecretWithExistingValueNoop(t *testing.T) {
	base := map[string]string{"SECRET_KEY": "shhh"}
	set := map[string]string{"SECRET_KEY": MaskedSecret}

	merged, diffs, err := MergeEnv(base, set, nil, MergeOptions{
		AllowSecretUpdates: true,
		IsSecretKey: func(key string) bool {
			return key == "SECRET_KEY"
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if merged["SECRET_KEY"] != "shhh" {
		t.Fatalf("expected existing secret to be preserved, got %s", merged["SECRET_KEY"])
	}

	if len(diffs) != 0 {
		t.Fatalf("expected no diffs when masked secret is submitted, got %d", len(diffs))
	}
}

func TestMergeEnvMaskedSecretByViewerNoSecretPermission(t *testing.T) {
	base := map[string]string{"SECRET_KEY": "shhh", "FOO": "bar"}
	set := map[string]string{"SECRET_KEY": MaskedSecret, "FOO": "baz"}

	merged, diffs, err := MergeEnv(base, set, nil, MergeOptions{
		AllowSecretUpdates: false,
		IsSecretKey: func(key string) bool {
			return key == "SECRET_KEY"
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if merged["SECRET_KEY"] != "shhh" {
		t.Fatalf("expected secret to remain unchanged, got %s", merged["SECRET_KEY"])
	}

	if merged["FOO"] != "baz" {
		t.Fatalf("expected non-secret to update, got %s", merged["FOO"])
	}

	if len(diffs) != 1 || diffs[0].Key != "FOO" {
		t.Fatalf("expected single diff for FOO, got %#v", diffs)
	}
}

func TestMergeEnvRejectsLineBreakInjection(t *testing.T) {
	base := map[string]string{"DATABASE_URL": "postgres://real/db"}
	protected := func(key string) bool { return key == "DATABASE_URL" }

	for name, set := range map[string]map[string]string{
		"newline in value":         {"ZZZ_NOTE": "x\nDATABASE_URL=postgres://evil/x"},
		"carriage return in value": {"ZZZ_NOTE": "x\rDATABASE_URL=postgres://evil/x"},
		"NUL in value":             {"ZZZ_NOTE": "x\x00y"},
		"equals in key":            {"A=B": "x"},
		"newline in key":           {"A\nDATABASE_URL": "x"},
		"leading digit key":        {"1ABC": "x"},
	} {
		_, _, err := MergeEnv(base, set, nil, MergeOptions{IsProtectedKey: protected})
		if !errors.Is(err, ErrInvalidEnvEntry) {
			t.Fatalf("%s: expected ErrInvalidEnvEntry, got %v", name, err)
		}
	}

	_, _, err := MergeEnv(base, nil, []string{"BAD KEY"}, MergeOptions{})
	if !errors.Is(err, ErrInvalidEnvEntry) {
		t.Fatalf("remove with invalid key: expected ErrInvalidEnvEntry, got %v", err)
	}
}

func TestMergeEnvAllowsOrdinaryValues(t *testing.T) {
	merged, _, err := MergeEnv(map[string]string{}, map[string]string{
		"_PRIVATE":  "ok",
		"FEATURE_X": "a=b; c, d=\"e\" \t tab",
	}, nil, MergeOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if merged["FEATURE_X"] != "a=b; c, d=\"e\" \t tab" {
		t.Fatalf("value changed: %q", merged["FEATURE_X"])
	}
}
