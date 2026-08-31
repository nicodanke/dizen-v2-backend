// Command keygen prints a fresh Ed25519 key pair, in the shape the destination needs.
//
// There are two shapes and they are not interchangeable, which is the whole reason the flag
// exists:
//
//	keygen         a single line with \n escapes, for a .env file. godotenv expands the
//	               escapes when it reads the file, so the service receives a real PEM.
//	keygen -pem    the PEM as it is, with real newlines, for a secrets manager such as
//	               Doppler. Nothing expands anything there: the value arrives as an
//	               environment variable exactly as it was stored.
//
// Pasting the escaped form into a secrets manager fails in two different ways, and only one
// of them is loud: identity refuses to start, because an unparseable private key is an
// error, while the four services that only verify load a key set with no keys in it and
// then reject every token they are given. That silent half is what this flag prevents.
//
// The key never touches the repository (hard rule 6), in any environment, and each
// environment gets its own pair.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/nicodanke/dizen-v2-backend/pkg/jwt"
)

func main() {
	raw := flag.Bool("pem", false, "print real PEM, for a secrets manager, instead of the escaped .env form")
	flag.Parse()

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

	if *raw {
		fmt.Printf("# JWT_PRIVATE_KEY_PEM  --  identity only, copy everything between the markers\n%s\n",
			strings.TrimSpace(string(privatePEM)))
		fmt.Printf("\n# JWT_PUBLIC_KEYS_PEM  --  every service\n%s\n",
			strings.TrimSpace(string(publicPEM)))

		return
	}

	fmt.Printf("# identity only:\nJWT_PRIVATE_KEY_PEM=\"%s\"\n\n", escape(privatePEM))
	fmt.Printf("# every service:\nJWT_PUBLIC_KEYS_PEM=\"%s\"\n\n", escape(publicPEM))
	fmt.Print("# This form is for a .env file only. For Doppler or any other secrets manager\n")
	fmt.Print("# run it again with -pem: the escaped form does not parse there.\n")
}

// escape renders a PEM block as a single-line value a .env file can hold.
func escape(pem []byte) string {
	return strings.ReplaceAll(strings.TrimSpace(string(pem)), "\n", "\\n")
}
