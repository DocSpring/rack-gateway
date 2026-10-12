package envutil

import (
	"errors"
	"strings"
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

// A caller who can't read a secret or protected value must not learn whether a guess is right: submitting the
// current value is refused like any other value, where it used to be accepted as "no change".
func TestMergeEnvRefusesUnchangedSecretAndProtectedValues(t *testing.T) {
	base := map[string]string{"SECRET_KEY": "shhh", "ADMIN_PASSWORD": "hunter2"}
	opts := MergeOptions{
		IsSecretKey:    func(key string) bool { return key == "SECRET_KEY" },
		IsProtectedKey: func(key string) bool { return key == "ADMIN_PASSWORD" },
	}

	_, _, err := MergeEnv(base, map[string]string{"SECRET_KEY": "shhh"}, nil, opts)
	if !errors.Is(err, ErrSecretPermission) {
		t.Fatalf("secret resubmitted without secrets:set: expected ErrSecretPermission, got %v", err)
	}

	opts.AllowSecretUpdates = true
	_, _, err = MergeEnv(base, map[string]string{"ADMIN_PASSWORD": "hunter2"}, nil, opts)
	if !errors.Is(err, ErrProtectedEnvModification) {
		t.Fatalf("protected value resubmitted: expected ErrProtectedEnvModification, got %v", err)
	}
	_, _, err = MergeEnv(base, map[string]string{"ADMIN_PASSWORD": MaskedSecret}, nil, opts)
	if !errors.Is(err, ErrProtectedEnvModification) {
		t.Fatalf("protected value masked: expected ErrProtectedEnvModification, got %v", err)
	}

	// With secrets:set, resubmitting a secret's value is still allowed and changes nothing.
	merged, diffs, err := MergeEnv(base, map[string]string{"SECRET_KEY": "shhh"}, nil, opts)
	if err != nil || merged["SECRET_KEY"] != "shhh" || len(diffs) != 0 {
		t.Fatalf("secret resubmitted with secrets:set: merged=%v diffs=%v err=%v", merged, diffs, err)
	}
}

// `convox env` prints values to other users' terminals, so values can't carry escape sequences or other control
// characters. Tab is allowed.
func TestValidateEnvEntryRefusesControlCharacters(t *testing.T) {
	for name, value := range map[string]string{
		"escape sequence": "hi\x1b[2J",
		"bell":            "a\ab",
		"backspace":       "secret\b\b\b\bpublic",
		"DEL":             "a\x7fb",
		"C1 CSI":          "a\u009b2J",
		"invalid UTF-8":   "a\xffb",
	} {
		if err := ValidateEnvEntry("BANNER", value); !errors.Is(err, ErrInvalidEnvEntry) {
			t.Errorf("%s: expected ErrInvalidEnvEntry, got %v", name, err)
		}
	}
	if err := ValidateEnvEntry("BANNER", "tab\tand ünïcode"); err != nil {
		t.Errorf("tab and non-ASCII letters: unexpected error %v", err)
	}
	err := ValidateEnvEntry("BAD\x1bKEY", "x")
	if !errors.Is(err, ErrInvalidEnvEntry) || strings.ContainsRune(err.Error(), '\x1b') {
		t.Errorf("control character in key: want ErrInvalidEnvEntry naming the key escaped, got %q", err)
	}
}
