package envutil

import "testing"

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
