package auth

import (
	"crypto/sha256"
	"encoding/hex"
)

const LocalPrincipalKeyHash = "local-development"

type AccessPrincipal struct {
	KeyHash string
	Kind    string
}

func (p AccessPrincipal) IsZero() bool {
	return p.KeyHash == "" && p.Kind == ""
}

func NewAccessPrincipal(accessKey string, kind string) AccessPrincipal {
	if accessKey == "" {
		return AccessPrincipal{KeyHash: LocalPrincipalKeyHash, Kind: kind}
	}
	sum := sha256.Sum256([]byte(accessKey))
	return AccessPrincipal{KeyHash: hex.EncodeToString(sum[:]), Kind: kind}
}
