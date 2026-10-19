package auth

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sync"
	"time"
)

const (
	jwksTTL      = 10 * time.Minute // how long a fetched key set is trusted
	jwksMinRetry = 30 * time.Second // never refetch more often than this (stops abuse via fake key ids)
)

type jwk struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	Kid string `json:"kid"`
	X   string `json:"x"`
	Y   string `json:"y"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// jwksCache fetches and caches the public keys Supabase signs tokens with.
type jwksCache struct {
	url    string
	client *http.Client

	mu          sync.Mutex
	keys        map[string]crypto.PublicKey
	fetchedAt   time.Time
	lastAttempt time.Time
}

func newJWKSCache(url string, client *http.Client) *jwksCache {
	return &jwksCache{url: url, client: client}
}

func (c *jwksCache) key(ctx context.Context, kid string) (crypto.PublicKey, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if k, ok := c.keys[kid]; ok && time.Since(c.fetchedAt) < jwksTTL {
		return k, nil
	}
	if time.Since(c.lastAttempt) >= jwksMinRetry {
		c.lastAttempt = time.Now()
		if err := c.fetch(ctx); err != nil {
			// A Supabase hiccup shouldn't lock everyone out: keep using a key we already have.
			if k, ok := c.keys[kid]; ok {
				return k, nil
			}
			return nil, err
		}
		c.fetchedAt = time.Now()
	}
	if k, ok := c.keys[kid]; ok {
		return k, nil
	}
	return nil, fmt.Errorf("unknown signing key id %q", kid)
}

func (c *jwksCache) fetch(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch JWKS: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch JWKS: status %d", resp.StatusCode)
	}
	var set struct {
		Keys []jwk `json:"keys"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&set); err != nil {
		return fmt.Errorf("decode JWKS: %w", err)
	}
	keys := make(map[string]crypto.PublicKey, len(set.Keys))
	for _, k := range set.Keys {
		if k.Kid == "" {
			continue
		}
		if pub, err := k.publicKey(); err == nil {
			keys[k.Kid] = pub
		}
	}
	if len(keys) == 0 {
		return errors.New("JWKS contained no usable keys")
	}
	c.keys = keys
	return nil
}

func (k jwk) publicKey() (crypto.PublicKey, error) {
	switch k.Kty {
	case "EC":
		if k.Crv != "P-256" {
			return nil, fmt.Errorf("unsupported curve %q", k.Crv)
		}
		x, err := b64Int(k.X)
		if err != nil {
			return nil, err
		}
		y, err := b64Int(k.Y)
		if err != nil {
			return nil, err
		}
		curve := elliptic.P256()
		if !curve.IsOnCurve(x, y) {
			return nil, errors.New("EC point is not on the curve")
		}
		return &ecdsa.PublicKey{Curve: curve, X: x, Y: y}, nil
	case "RSA":
		n, err := b64Int(k.N)
		if err != nil {
			return nil, err
		}
		e, err := b64Int(k.E)
		if err != nil {
			return nil, err
		}
		if !e.IsInt64() || e.Int64() < 3 || e.Int64() > 1<<31-1 {
			return nil, errors.New("bad RSA exponent")
		}
		return &rsa.PublicKey{N: n, E: int(e.Int64())}, nil
	}
	return nil, fmt.Errorf("unsupported key type %q", k.Kty)
}

func b64Int(s string) (*big.Int, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(b) == 0 {
		return nil, errors.New("bad base64url number in JWK")
	}
	return new(big.Int).SetBytes(b), nil
}
