package auth

type AccessPrincipal struct {
	KeyHash string
	Kind    string
}

func (p AccessPrincipal) IsZero() bool {
	return p.KeyHash == "" && p.Kind == ""
}
