package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"log"
	"regexp"
	"strings"
	"time"
)

var (
	reAPIKeyID     = regexp.MustCompile(`^hc_[0-9a-f]{8}$`)
	reIngestStrict = regexp.MustCompile(
		`^` +
			`(?P<ts>\d{8}\.\d{6})` +
			`\s*-\s*` +
			`(?:(?P<sid>[0-9a-f]{8})\s*-\s*)?` +
			`(?P<host>[A-Za-z0-9._-]+)` +
			`(?:\s+\[cwd=(?P<cwd>[^\]]+)\])?` +
			`\s+>\s+` +
			`(?P<payload>.*)` +
			`$`,
	)
)

func (s *IngestService) authAPIKeyFromLine(msg *RawMsg) *Tenant {
	debugPrint(log.Printf, levelCrazy, "Extract payload from the strict ingest line %s\n", msg.Line)
	payload, rest, ok := separatePayloadStrict(msg.Line)
	if !ok {
		return nil
	}

	token, cleaned, ok := ExtractAPIKeyTokenFromPayload(payload)
	if !ok {
		return nil
	}
	debugPrint(log.Printf, levelCrazy, "Extracted api key token '%s'\n", token)

	msg.Line = rest + cleaned
	debugPrint(log.Printf, levelCrazy, "Cleaned line '%s'\n", msg.Line)
	keyID, secret, ok := splitKeyToken(token)
	if !ok {
		return nil
	}
	debugPrint(log.Printf, levelCrazy, "token parts: key_id=%s Hash=%s\n", keyID, secret)

	if !reAPIKeyID.MatchString(keyID) {
		return nil
	}
	if len(secret) < 16 || len(secret) > 128 {
		return nil
	}

	debugPrint(log.Printf, levelCrazy, "Lookup key in DB and verify\n")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	rec, ok, err := s.db.GetAPIKeyByKeyID(ctx, keyID)

	if err != nil || !ok {
		return nil
	}

	debugPrint(log.Printf, levelCrazy, "rec.TenantID='%s', KeyHash='%s', Revoked='%v'\n", rec.TenantID, rec.KeyHash, rec.Revoked)

	if rec.Revoked.Valid {
		debugPrint(log.Printf, levelDebug, "key explicitly revoked\n")
		return nil
	}

	debugPrint(log.Printf, levelCrazy, "Key_id exists\n")

	pepper := s.cfg.AppCfg.Globals.Pepper
	if !verifySecretSHA256(secret, pepper, rec.KeyHash) {
		return nil
	}

	debugPrint(log.Printf, levelCrazy, "tenant=%s, msg=%v\n", rec.TenantID, msg)

	return s.getTenantPTR(rec.TenantID)
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

func hashSecretSHA256(secret, pepper string) string {
	h := sha256.New()
	h.Write([]byte(secret))
	h.Write([]byte(":"))
	h.Write([]byte(pepper))
	return hex.EncodeToString(h.Sum(nil))
}

func verifySecretSHA256(secret, pepper, storedHash string) bool {
	sum := hashSecretSHA256(secret, pepper)
	return subtle.ConstantTimeCompare([]byte(sum), []byte(storedHash)) == 1
}
