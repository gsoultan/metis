// Package fcm sends push notifications through Firebase Cloud Messaging's
// HTTP v1 API.
//
// It signs in with a Firebase service account the way Google's own libraries
// do — a JWT signed with the account's private key, traded for a short-lived
// access token — using only the standard library and the JWT package the
// module already depends on, so push adds no dependency.
//
// The endpoints are fixed to Google's. A service account file carries a
// token_uri of its own, and honouring it would let whoever pasted the file
// point the server's signed assertion at any host; the file is read for the
// account's identity and key, nothing else.
package fcm
