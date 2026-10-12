package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"

	"github.com/Dynamisch-LLC/agentgate/internal/decision"
)

// RedactionMode represents how an argument attribute is sanitized before audit persistence.
type RedactionMode string

const (
	// RedactFull persists the argument value as-is.
	RedactFull RedactionMode = "full"

	// RedactHash replaces the argument value with a salted SHA-256 hash.
	RedactHash RedactionMode = "hash"

	// RedactOmit removes or masks the argument value with [REDACTED].
	RedactOmit RedactionMode = "omit"
)

var sensitiveKeywords = []string{
	"password",
	"token",
	"secret",
	"key",
	"auth",
	"credential",
	"private",
	"cert",
	"ssn",
	"cvv",
}

// Redactor sanitizes tool arguments before canonical serialization and audit persistence.
type Redactor struct {
	salt  string
	rules map[string]map[string]RedactionMode // toolName -> argName -> mode
}

// NewRedactor constructs a Redactor with the given salt and per-tool argument rules.
// If salt is empty, it checks AGENTGATE_AUDIT_SALT from the environment.
func NewRedactor(salt string, rules map[string]map[string]RedactionMode) *Redactor {
	if salt == "" {
		salt = os.Getenv("AGENTGATE_AUDIT_SALT")
		if salt == "" {
			if file := os.Getenv("AGENTGATE_AUDIT_SALT_FILE"); file != "" {
				if data, err := os.ReadFile(strings.TrimSpace(file)); err == nil {
					salt = strings.TrimSpace(string(data))
				}
			}
		}
		if salt == "" {
			salt = "default-agentgate-audit-salt"
		}
	}
	if rules == nil {
		rules = make(map[string]map[string]RedactionMode)
	}
	return &Redactor{
		salt:  salt,
		rules: rules,
	}
}

// RedactArguments sanitizes all raw arguments for a tool before persistence.
// Raw bearer tokens and sensitive credentials are never disclosed.
func (r *Redactor) RedactArguments(toolName string, rawArgs map[string]decision.AttributeValue) map[string]string {
	out := make(map[string]string, len(rawArgs))
	if len(rawArgs) == 0 {
		return out
	}

	toolRules := r.rules[toolName]
	wildcardRules := r.rules["*"]

	for k, v := range rawArgs {
		val := v.String()

		// 1. Determine configured mode
		mode := RedactFull
		if toolRules != nil && toolRules[k] != "" {
			mode = toolRules[k]
		} else if wildcardRules != nil && wildcardRules[k] != "" {
			mode = wildcardRules[k]
		} else if isSensitiveKey(k) {
			mode = RedactOmit
		}

		// 2. Non-disclosing safety invariant: sensitive keys or bearer tokens cannot be RedactFull
		if isSensitiveKey(k) && mode == RedactFull {
			mode = RedactOmit
		}
		if isBearerOrSecretContent(val) {
			mode = RedactOmit
		}

		// 3. Apply mode
		switch mode {
		case RedactOmit:
			out[k] = "[REDACTED]"
		case RedactHash:
			h := sha256.Sum256([]byte(r.salt + ":" + val))
			out[k] = hex.EncodeToString(h[:])
		case RedactFull:
			out[k] = val
		default:
			out[k] = "[REDACTED]"
		}
	}

	return out
}

func isSensitiveKey(name string) bool {
	lower := strings.ToLower(name)
	for _, kw := range sensitiveKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

func isBearerOrSecretContent(val string) bool {
	trimmed := strings.TrimSpace(val)
	if strings.HasPrefix(trimmed, "Bearer ") {
		return true
	}
	// Detect common JWT pattern
	if strings.Contains(trimmed, "eyJhbGci") {
		return true
	}
	return false
}
