package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"golang.org/x/crypto/ssh"
)

func generateTestKey(t *testing.T) (ssh.Signer, ssh.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate ed25519 key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("create ssh signer: %v", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("create ssh public key: %v", err)
	}
	return signer, sshPub
}

func TestSignAndVerify(t *testing.T) {
	signer, pubKey := generateTestKey(t)

	data := []byte("test-nonce-value")
	sig, err := SignSSH(signer, data)
	if err != nil {
		t.Fatalf("SignSSH returned error: %v", err)
	}
	if len(sig) == 0 {
		t.Fatal("SignSSH returned empty signature")
	}

	if err := VerifySSH(pubKey, data, sig); err != nil {
		t.Fatalf("VerifySSH returned error: %v", err)
	}
}

func TestVerifyWrongKey(t *testing.T) {
	signer, _ := generateTestKey(t)
	_, otherPub := generateTestKey(t)

	data := []byte("test-nonce-value")
	sig, err := SignSSH(signer, data)
	if err != nil {
		t.Fatalf("SignSSH returned error: %v", err)
	}

	if err := VerifySSH(otherPub, data, sig); err == nil {
		t.Fatal("VerifySSH should fail with wrong public key")
	}
}

func TestVerifyWrongData(t *testing.T) {
	signer, pubKey := generateTestKey(t)

	data := []byte("correct-data")
	sig, err := SignSSH(signer, data)
	if err != nil {
		t.Fatalf("SignSSH returned error: %v", err)
	}

	wrongData := []byte("wrong-data")
	if err := VerifySSH(pubKey, wrongData, sig); err == nil {
		t.Fatal("VerifySSH should fail with wrong data")
	}
}
