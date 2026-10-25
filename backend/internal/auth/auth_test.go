package auth

import (
	"context"
	"testing"
)

func TestSingleUser(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef"
	authenticator := NewSingleUser("01JB8Z6Q4M1X2Y3Z4A5B6C7D8E", HashToken(token))

	t.Run("accepts the token of the user", func(t *testing.T) {
		userID, ok := authenticator.Authenticate(token)
		if !ok || userID != "01JB8Z6Q4M1X2Y3Z4A5B6C7D8E" {
			t.Errorf("Authenticate() = %q, %v; want the user, true", userID, ok)
		}
	})

	for _, tc := range []struct{ name, token string }{
		{"rejects an empty token", ""},
		{"rejects a wrong token", "fedcba9876543210fedcba9876543210"},
		{"rejects the hash used as a token", HashToken(token)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if userID, ok := authenticator.Authenticate(tc.token); ok || userID != "" {
				t.Errorf("Authenticate() = %q, %v; want \"\", false", userID, ok)
			}
		})
	}
}

func TestUserID(t *testing.T) {
	if _, ok := UserID(context.Background()); ok {
		t.Error("UserID on an empty context: want ok = false")
	}
	userID, ok := UserID(WithUserID(context.Background(), "u1"))
	if !ok || userID != "u1" {
		t.Errorf("UserID() = %q, %v; want u1, true", userID, ok)
	}
}
