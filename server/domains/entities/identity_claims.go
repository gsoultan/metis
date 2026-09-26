package entities

// IdentityClaims is what an identity provider vouched for in a verified ID
// token: who somebody is to that provider, and what the claim an operator named
// says about the organizations they belong to.
//
// Issuer and Subject together are the identity, and nothing else here is. A
// username or an email address is a label a provider may reissue, and an email
// claim is no proof of the same person across two providers — so the account a
// sign-in reaches is found by the pair, never by either of those.
type IdentityClaims struct {
	Issuer   string
	Subject  string
	Username string
	Name     string
	Email    string

	// OrganizationClaim is the claim an operator said carries a person's
	// organizations. Empty when none was named.
	OrganizationClaim string
	// HasOrganizationClaim reports whether the token carried that claim.
	HasOrganizationClaim bool
	// Organizations are the claim's values: the one a string held, or each
	// string a list held.
	Organizations []string
}
