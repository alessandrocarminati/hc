package main

import (
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

const (
	versionByte   = 0x01
	x25519PubSize = 32
	saltSize      = 16
	nonceSize     = chacha20poly1305.NonceSizeX
)

func genAsymKey() (string, string, error) {
	curve := ecdh.X25519()

	priv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	privRaw := priv.Bytes()
	pubRaw := priv.PublicKey().Bytes()
	privB64 := base64.StdEncoding.EncodeToString(privRaw)
	pubB64 := base64.StdEncoding.EncodeToString(pubRaw)
	return privB64, pubB64, nil
}

func cryptString(message string, recipientPubKey []byte) (string, error) {
	if len(recipientPubKey) != x25519PubSize {
		return "", fmt.Errorf("recipient public key must be %d bytes (raw X25519)", x25519PubSize)
	}

	curve := ecdh.X25519()

	peerPub, err := curve.NewPublicKey(recipientPubKey)
	if err != nil {
		return "", fmt.Errorf("invalid recipient public key: %w", err)
	}

	ephemeralPriv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return "", fmt.Errorf("generate ephemeral key: %w", err)
	}
	ephemeralPub := ephemeralPriv.PublicKey().Bytes()

	sharedSecret, err := ephemeralPriv.ECDH(peerPub)
	if err != nil {
		return "", fmt.Errorf("ecdh: %w", err)
	}

	salt := make([]byte, saltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return "", fmt.Errorf("salt: %w", err)
	}

	info := []byte("hc-crypt-v1")
	h := hkdf.New(sha256.New, sharedSecret, salt, info)

	aeadKey := make([]byte, chacha20poly1305.KeySize)
	if _, err := io.ReadFull(h, aeadKey); err != nil {
		return "", fmt.Errorf("hkdf: %w", err)
	}

	aead, err := chacha20poly1305.NewX(aeadKey)
	if err != nil {
		return "", fmt.Errorf("aead: %w", err)
	}

	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("nonce: %w", err)
	}

	plaintext := []byte(message)

	header := make([]byte, 0, 1+x25519PubSize+saltSize+nonceSize)
	header = append(header, versionByte)
	header = append(header, ephemeralPub...)
	header = append(header, salt...)
	header = append(header, nonce...)

	ciphertext := aead.Seal(nil, nonce, plaintext, header)

	blob := append(header, ciphertext...)

	return base64.StdEncoding.EncodeToString(blob), nil
}

func decryptString(artifactB64 string, recipientPrivKey []byte) (string, error) {
	if len(recipientPrivKey) != x25519PubSize {
		return "", fmt.Errorf("recipient private key must be %d bytes (raw X25519)", x25519PubSize)
	}

	blob, err := base64.StdEncoding.DecodeString(artifactB64)
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}

	minLen := 1 + x25519PubSize + saltSize + nonceSize + 16
	if len(blob) < minLen {
		return "", errors.New("artifact too short")
	}

	ver := blob[0]
	if ver != versionByte {
		return "", fmt.Errorf("unsupported version: %d", ver)
	}

	offset := 1
	ephemeralPub := blob[offset : offset+x25519PubSize]
	offset += x25519PubSize

	salt := blob[offset : offset+saltSize]
	offset += saltSize

	nonce := blob[offset : offset+nonceSize]
	offset += nonceSize

	header := blob[:offset]
	ciphertext := blob[offset:]

	curve := ecdh.X25519()

	priv, err := curve.NewPrivateKey(recipientPrivKey)
	if err != nil {
		return "", fmt.Errorf("invalid recipient private key: %w", err)
	}

	peerPub, err := curve.NewPublicKey(ephemeralPub)
	if err != nil {
		return "", fmt.Errorf("invalid ephemeral public key: %w", err)
	}

	sharedSecret, err := priv.ECDH(peerPub)
	if err != nil {
		return "", fmt.Errorf("ecdh: %w", err)
	}

	info := []byte("hc-crypt-v1")
	h := hkdf.New(sha256.New, sharedSecret, salt, info)

	aeadKey := make([]byte, chacha20poly1305.KeySize)
	if _, err := io.ReadFull(h, aeadKey); err != nil {
		return "", fmt.Errorf("hkdf: %w", err)
	}

	aead, err := chacha20poly1305.NewX(aeadKey)
	if err != nil {
		return "", fmt.Errorf("aead: %w", err)
	}

	plaintext, err := aead.Open(nil, nonce, ciphertext, header)
	if err != nil {
		return "", fmt.Errorf("decrypt/auth failed: %w", err)
	}

	return string(plaintext), nil
}
