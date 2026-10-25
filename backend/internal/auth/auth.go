// Package auth checks the static API token and carries the authenticated user in the request
// context (ADR-0016, ADR-0022).
package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
)

// HashToken returns the SHA-256 of a token, hex-encoded. Only the hash is stored (users table).
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// SingleUser authenticates the one V1 user: a request is accepted when its token hashes to the
// stored hash. Comparing fixed-length hashes in constant time leaks nothing about the token.
type SingleUser struct {
	userID    string
	tokenHash []byte
}

// NewSingleUser returns an authenticator for userID, whose token hashes to tokenHash.
func NewSingleUser(userID, tokenHash string) SingleUser {
	return SingleUser{userID: userID, tokenHash: []byte(tokenHash)}
}

// Authenticate returns the user owning token, and whether the token is valid.
func (a SingleUser) Authenticate(token string) (string, bool) {
	if token == "" {
		return "", false
	}
	if subtle.ConstantTimeCompare([]byte(HashToken(token)), a.tokenHash) != 1 {
		return "", false
	}
	return a.userID, true
}

type userIDKey struct{}

// WithUserID returns a context carrying the authenticated user.
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDKey{}, userID)
}

// UserID returns the authenticated user of ctx. Every query on a business table filters by it
// (RG-90); ok is false outside an authenticated request.
func UserID(ctx context.Context) (string, bool) {
	userID, ok := ctx.Value(userIDKey{}).(string)
	return userID, ok && userID != ""
}
