package main

import "testing"

// The master token must keep working exactly as before.
func TestServeAuthMasterToken(t *testing.T) {
	secret := "838473502c4742723875d3ee6d152b4caaa87c3a61e395f4"
	if !serveAuth(secret, secret) {
		t.Fatal("master token should authenticate")
	}
	if serveAuth("wrong", secret) {
		t.Fatal("garbage token must be rejected")
	}
}

// A sha256(token) secret (self-hosted setups) keeps working.
func TestServeAuthSha256Form(t *testing.T) {
	secret := sha256hex("supersecret")
	if !serveAuth("supersecret", secret) {
		t.Fatal("sha256 token form should authenticate")
	}
}

// The public token is accepted for itself and NOT as the master secret.
func TestServeAuthPublicToken(t *testing.T) {
	master := "838473502c4742723875d3ee6d152b4caaa87c3a61e395f4"
	public := "d0e1f2a3b4c5d6e7f8091a2b3c4d5e6f"

	if serveAuth(public, master) {
		t.Fatal("public token must not authenticate against the master secret")
	}
	if !serveAuth(public, public) {
		t.Fatal("public token should authenticate against the public secret")
	}
}

// Obfuscation round-trips, so the client can ship the public token obfuscated.
func TestTokenObfuscationRoundTrip(t *testing.T) {
	public := "d0e1f2a3b4c5d6e7f8091a2b3c4d5e6f"
	obf, err := tokenObfuscate(ObfuscationPass, public)
	if err != nil {
		t.Fatalf("obfuscate: %v", err)
	}
	if obf == public {
		t.Fatal("obfuscated form must differ from plaintext")
	}
	plain, err := tokenDeobfuscate(ObfuscationPass, obf)
	if err != nil {
		t.Fatalf("deobfuscate: %v", err)
	}
	if plain != public {
		t.Fatalf("round trip mismatch: got %q want %q", plain, public)
	}
	// the deobfuscated value is what the server finally compares
	if !serveAuth(plain, public) {
		t.Fatal("deobfuscated public token should authenticate")
	}
}

// An empty server secret must never authenticate anything.
func TestServeAuthEmptySecret(t *testing.T) {
	if serveAuth("", "") || serveAuth("x", "") || serveAuth("", "x") {
		t.Fatal("empty secrets must be rejected")
	}
}
