package workbench

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"strings"
	"sync"
	"time"
)

const bootstrapNonceTTL = time.Minute

// ErrBootstrapDenied deliberately covers malformed, expired, replayed, and
// unknown nonces so callers do not gain an oracle about the bootstrap state.
var ErrBootstrapDenied = errors.New("workbench bootstrap denied")

// BootstrapAuthority holds one process-local nonce-to-bearer exchange. It keeps
// only a digest of the URL-carried nonce and never persists either credential.
type BootstrapAuthority struct {
	mu        sync.Mutex
	now       func() time.Time
	expiresAt time.Time
	nonceHash [sha256.Size]byte
	bearer    string
	consumed  bool
}

// NewBootstrapAuthority creates a 60-second, single-use bootstrap exchange.
func NewBootstrapAuthority() (*BootstrapAuthority, string, error) {
	return newBootstrapAuthority(time.Now, bootstrapNonceTTL, NewBearerToken)
}

func newBootstrapAuthority(now func() time.Time, ttl time.Duration, nextToken func() (string, error)) (*BootstrapAuthority, string, error) {
	if now == nil {
		return nil, "", errors.New("workbench bootstrap clock is required")
	}
	if ttl <= 0 {
		return nil, "", errors.New("workbench bootstrap TTL must be positive")
	}
	if nextToken == nil {
		return nil, "", errors.New("workbench bootstrap token generator is required")
	}
	nonce, err := nextToken()
	if err != nil {
		return nil, "", err
	}
	bearer, err := nextToken()
	if err != nil {
		return nil, "", err
	}
	if nonce == bearer {
		return nil, "", errors.New("workbench bootstrap credentials must be distinct")
	}
	return &BootstrapAuthority{
		now:       now,
		expiresAt: now().Add(ttl),
		nonceHash: sha256.Sum256([]byte(nonce)),
		bearer:    bearer,
	}, nonce, nil
}

// consume atomically invalidates a valid nonce before returning the bearer.
func (authority *BootstrapAuthority) consume(nonce string) (string, error) {
	digest := sha256.Sum256([]byte(nonce))
	authority.mu.Lock()
	defer authority.mu.Unlock()
	now := authority.now()
	expired := !now.Before(authority.expiresAt)
	if authority.consumed || expired || subtle.ConstantTimeCompare(digest[:], authority.nonceHash[:]) != 1 {
		if expired {
			authority.consumed = true
			authority.nonceHash = [sha256.Size]byte{}
		}
		return "", ErrBootstrapDenied
	}
	authority.consumed = true
	authority.nonceHash = [sha256.Size]byte{}
	return authority.bearer, nil
}

func (authority *BootstrapAuthority) authorizes(header string) bool {
	const prefix = "Bearer "
	authority.mu.Lock()
	defer authority.mu.Unlock()
	if !authority.consumed || len(header) != len(prefix)+len(authority.bearer) || !strings.HasPrefix(header, prefix) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(header[len(prefix):]), []byte(authority.bearer)) == 1
}
