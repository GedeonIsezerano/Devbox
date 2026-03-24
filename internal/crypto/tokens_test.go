package crypto

import (
	"strings"
	"testing"
)

func TestGenerateToken(t *testing.T) {
	token, err := GenerateToken("dbx_pat_")
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}
	if token == "" {
		t.Fatal("GenerateToken returned empty string")
	}
}

func TestGenerateTokenPAT(t *testing.T) {
	token, err := GenerateToken("dbx_pat_")
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}
	if !strings.HasPrefix(token, "dbx_pat_") {
		t.Fatalf("expected token to have prefix 'dbx_pat_', got %q", token)
	}
}

func TestGenerateTokenProvision(t *testing.T) {
	token, err := GenerateToken("dbx_prov_")
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}
	if !strings.HasPrefix(token, "dbx_prov_") {
		t.Fatalf("expected token to have prefix 'dbx_prov_', got %q", token)
	}
}

func TestHashToken(t *testing.T) {
	token := "dbx_pat_testtoken123"
	hash1 := HashToken(token)
	hash2 := HashToken(token)
	if hash1 != hash2 {
		t.Fatalf("HashToken is not deterministic: %q != %q", hash1, hash2)
	}
	if hash1 == "" {
		t.Fatal("HashToken returned empty string")
	}
}

func TestTokensAreUnique(t *testing.T) {
	token1, err := GenerateToken("dbx_pat_")
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}
	token2, err := GenerateToken("dbx_pat_")
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}
	if token1 == token2 {
		t.Fatal("two generated tokens should differ")
	}
}
