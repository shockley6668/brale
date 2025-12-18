package trader

import (
	"strings"

	symbolpkg "brale/internal/pkg/symbol"
)

func normalizeSymbol(raw string) string {
	if norm := symbolpkg.Normalize(raw); norm != "" {
		return norm
	}
	return strings.ToUpper(strings.TrimSpace(raw))
}
