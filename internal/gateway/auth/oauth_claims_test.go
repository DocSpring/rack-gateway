package auth

import (
	"errors"
	"testing"
)

func TestIdentityClaimsValidate(t *testing.T) {
	cases := []struct {
		name          string
		claims        identityClaims
		allowedDomain string
		wantErr       bool
		wantDomainErr bool
	}{
		{
			"workspace user",
			identityClaims{Email: "a@docspring.com", EmailVerified: true, HD: "docspring.com"},
			"docspring.com", false, false,
		},
		{
			"domain case differs",
			identityClaims{Email: "a@DocSpring.com", EmailVerified: true, HD: "docspring.com"},
			"docspring.com", false, false,
		},
		{
			"unverified email",
			identityClaims{Email: "a@docspring.com", EmailVerified: false, HD: "docspring.com"},
			"docspring.com", true, false,
		},
		{
			"consumer account with matching email domain",
			identityClaims{Email: "a@docspring.com", EmailVerified: true},
			"docspring.com", true, true,
		},
		{
			"other workspace",
			identityClaims{Email: "a@evil.com", EmailVerified: true, HD: "evil.com"},
			"docspring.com", true, true,
		},
		{
			"hd mismatch",
			identityClaims{Email: "a@docspring.com", EmailVerified: true, HD: "evil.com"},
			"docspring.com", true, true,
		},
		{
			"subdomain",
			identityClaims{Email: "a@x.docspring.com", EmailVerified: true, HD: "x.docspring.com"},
			"docspring.com", true, true,
		},
		{
			"malformed email",
			identityClaims{Email: "a@b@docspring.com", EmailVerified: true, HD: "docspring.com"},
			"docspring.com", true, true,
		},
		{"no domain configured (dev)", identityClaims{Email: "a@gmail.com", EmailVerified: true}, "", false, false},
	}
	for _, tc := range cases {
		err := tc.claims.validate(tc.allowedDomain)
		if (err != nil) != tc.wantErr {
			t.Errorf("%s: err=%v wantErr=%v", tc.name, err, tc.wantErr)
		}
		var domainErr *DomainNotAllowedError
		if errors.As(err, &domainErr) != tc.wantDomainErr {
			t.Errorf("%s: DomainNotAllowedError=%v want %v", tc.name, errors.As(err, &domainErr), tc.wantDomainErr)
		}
	}
}
