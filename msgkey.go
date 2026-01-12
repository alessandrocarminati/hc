package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"regexp"
	"strings"
	"time"
	"log"
)

var reAPIKeyID = regexp.MustCompile(`^hc_[0-9a-f]{8}$`)

func (s *IngestService) authAPIKeyFromLine(msg *RawMsg) string {
	debugPrint(log.Printf, levelCrazy, "Extract payload from the strict ingest line %s\n", msg.Line)
	payload, rest, ok := separatePayloadStrict(msg.Line)
	if !ok {
		return ""
	}

	token, cleaned, ok := ExtractAPIKeyTokenFromPayload(payload)
	if !ok {
		return ""
	}
	debugPrint(log.Printf, levelCrazy, "Extracted api key token '%s'\n", token)

	msg.Line = rest + cleaned
	debugPrint(log.Printf, levelCrazy, "Cleaned line '%s'\n", msg.Line)
	keyID, secret, ok := splitKeyToken(token)
	if !ok {
		return ""
	}
	debugPrint(log.Printf, levelCrazy, "token parts: key_id=%s Hash=%s\n", keyID, secret)

	if !reAPIKeyID.MatchString(keyID) {
		return ""
	}
	if len(secret) < 16 || len(secret) > 128 {
		return ""
	}

	debugPrint(log.Printf, levelCrazy, "Lookup key in DB and verify\n")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var (
		tenantID string
		keyHash  string
		revoked  sql.NullTime
	)

	err := s.db.SQL.QueryRowContext(ctx, `
		select tenant_id::text, key_hash, revoked_at
		from api_keys
		where key_id = $1
	`, keyID).Scan(&tenantID, &keyHash, &revoked)
	if err != nil {
		return ""
	}
	if revoked.Valid {
		return ""
	}
	debugPrint(log.Printf, levelCrazy, "Key_id exists\n")

	pepper := strings.TrimSpace(s.cfg.Pepper)
	if !verifySecretSHA256(secret, pepper, keyHash) {
		return ""
	}

	debugPrint(log.Printf, levelCrazy, "msg =%v\n", msg)

	return strings.TrimSpace(tenantID)
}

func separatePayloadStrict(line string) (string, string, bool) {
	debugPrint(log.Printf, levelCrazy, "start line:  %s\n", line)
	m := reIngestStrict.FindStringSubmatch(strings.TrimSpace(line))
	if m == nil {
		return "", "", false
	}
	debugPrint(log.Printf, levelCrazy, "decomposed string: %v\n", m)
	idx := reIngestStrict.SubexpIndex("payload")
	if idx <= 0 || idx >= len(m) {
		return "", "", false
	}
	pos := strings.Index(line, m[idx])
	return m[idx], line[:pos] + line[pos+len(m[idx]):], true
}

func ExtractAPIKeyTokenFromPayload(payload string) (token string, cleaned string, ok bool) {
	debugPrint(log.Printf, levelCrazy, "Operate on %s\n", payload)
	p := strings.TrimSpace(payload)
	if p == "" {
		return "", payload, false
	}

	if strings.HasPrefix(p, "]apikey[") {
		debugPrint(log.Printf, levelCrazy, "api fingerprint is in the string let's check\n")
		rest := p[len("]apikey["):]
		end := strings.IndexByte(rest, ']')
		if end <= 0 {
			return "", payload, false
		}
		token = strings.TrimSpace(rest[:end])
		after := strings.TrimLeft(rest[end+1:], " \t")
		if token == "" {
			return "", payload, false
		}
		return token, after, true
	}

	if strings.HasPrefix(p, "]") {
		rest := p[1:]
		end := strings.IndexByte(rest, '[')
		if end <= 0 {
			return "", payload, false
		}
		token = strings.TrimSpace(rest[:end])
		after := strings.TrimLeft(rest[end+1:], " \t")
		if token == "" {
			return "", payload, false
		}
		return token, after, true
	}

	return "", payload, false
}

func splitKeyToken(tok string) (keyID, secret string, ok bool) {
	i := strings.IndexByte(tok, '.')
	if i <= 0 || i == len(tok)-1 {
		return "", "", false
	}
	return tok[:i], tok[i+1:], true
}

func verifySecretSHA256(secret, pepper, storedHash string) bool {
	h := sha256.New()
	h.Write([]byte(secret))
	h.Write([]byte(":"))
	h.Write([]byte(pepper))
	sum := hex.EncodeToString(h.Sum(nil))
	return subtle.ConstantTimeCompare([]byte(sum), []byte(storedHash)) == 1
}
