package crypto

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"golang.org/x/crypto/ssh"
)

const sshNamespace = "devbox-auth@v1"

// SignSSH signs data using the SSH key with namespace "devbox-auth@v1".
// The namespace is prepended to the hex-encoded data before signing so that
// both signer and verifier agree on the exact bytes that were signed.
// Returns the wire-format SSH signature bytes.
func SignSSH(signer ssh.Signer, data []byte) ([]byte, error) {
	signedData := buildSignedData(data)
	sig, err := signer.Sign(rand.Reader, signedData)
	if err != nil {
		return nil, fmt.Errorf("ssh sign: %w", err)
	}
	return ssh.Marshal(sig), nil
}

// VerifySSH verifies an SSH signature against the given public key and data
// using namespace "devbox-auth@v1". The same namespace-prefixed data
// construction used in SignSSH is applied before verification.
func VerifySSH(publicKey ssh.PublicKey, data []byte, signature []byte) error {
	var sig ssh.Signature
	if err := ssh.Unmarshal(signature, &sig); err != nil {
		return fmt.Errorf("unmarshal ssh signature: %w", err)
	}
	signedData := buildSignedData(data)
	return publicKey.Verify(signedData, &sig)
}

// buildSignedData constructs the data to be signed/verified by prepending the
// namespace to the hex-encoded data: "devbox-auth@v1:<hex(data)>".
func buildSignedData(data []byte) []byte {
	return []byte(sshNamespace + ":" + hex.EncodeToString(data))
}
