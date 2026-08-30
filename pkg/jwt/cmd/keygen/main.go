// Command keygen prints a fresh Ed25519 key pair in the shape the .env files expect.
//
// Development only: in staging and production the keys are managed as secrets by Dokploy
// and never touch the repository (hard rule 6).
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/nicodanke/dizen-v2-backend/pkg/jwt"
)

func main() {
	key, err := jwt.GenerateKey()
	if err != nil {
		fmt.Fprintf(os.Stderr, "generating the key: %v\n", err)
		os.Exit(1)
	}

	privatePEM, err := jwt.EncodePrivateKeyPEM(key)
	if err != nil {
		fmt.Fprintf(os.Stderr, "encoding the private key: %v\n", err)
		os.Exit(1)
	}

	publicPEM, err := jwt.EncodePublicKeyPEM(key)
	if err != nil {
		fmt.Fprintf(os.Stderr, "encoding the public key: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("# kid: %s\n\n", key.ID)
	fmt.Printf("# identity only:\nJWT_PRIVATE_KEY_PEM=\"%s\"\n\n", escape(privatePEM))
	fmt.Printf("# every service:\nJWT_PUBLIC_KEYS_PEM=\"%s\"\n", escape(publicPEM))
}

// escape renders a PEM block as a single-line value a .env file can hold.
func escape(pem []byte) string {
	return strings.ReplaceAll(strings.TrimSpace(string(pem)), "\n", "\\n")
}
