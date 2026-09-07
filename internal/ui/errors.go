package ui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/poizdev/lookup/internal/apperror"
)

type ErrorHint struct {
	Title, Detail string
	Fixes         []string
}

var errorHints = map[apperror.Kind]ErrorHint{
	apperror.KindAuthentication:         {"Authentication failed", "The provider rejected the configured credential.", []string{"Verify the API key with lookup init", "Check key permissions on the provider dashboard"}},
	apperror.KindRateLimit:              {"Rate limit exceeded", "The provider temporarily rejected requests.", []string{"Wait before trying again", "Run without AI: lookup --no-ai ."}},
	apperror.KindNetworkTimeout:         {"Connection timeout", "Lookup could not reach the provider within the request deadline.", []string{"Check your internet connection", "Run without AI: lookup --no-ai ."}},
	apperror.KindConfiguration:          {"Configuration error", "Lookup could not load or persist its configuration.", []string{"Check the config file syntax and permissions", "Re-run setup: lookup init"}},
	apperror.KindFilesystemPermission:   {"Permission denied", "Lookup cannot access the requested file or directory.", []string{"Check ownership and permissions", "Choose a user-writable path"}},
	apperror.KindPathNotFound:           {"Path not found", "The requested path does not exist.", []string{"Check the path", "Scan the current directory: lookup ."}},
	apperror.KindProviderInitialization: {"AI provider initialization failed", "The selected provider could not be configured.", []string{"Run setup wizard: lookup init", "Run without AI: lookup --no-ai ."}},
}

func RenderError(cap Capability, err error) string {
	if err == nil {
		return ""
	}
	var typed *apperror.Error
	if errors.As(err, &typed) {
		if hint, ok := errorHints[typed.Kind]; ok {
			return renderHint(cap, hint, err.Error())
		}
	}
	icons := Icons(cap.Unicode)
	return fmt.Sprintf("\n  %s Error: %s\n  |\n  | Possible fixes:\n  |   %s Run with --debug for detailed logs\n  |   %s Check https://github.com/poizdev/lookup/issues\n\n", icons.Error, err.Error(), icons.Bullet, icons.Bullet)
}

func renderHint(cap Capability, hint ErrorHint, detail string) string {
	icons := Icons(cap.Unicode)
	var b strings.Builder
	fmt.Fprintf(&b, "\n  %s Error: %s\n  | %s\n", icons.Error, hint.Title, hint.Detail)
	if detail != "" && detail != hint.Detail {
		fmt.Fprintf(&b, "  | %s\n", detail)
	}
	if len(hint.Fixes) > 0 {
		b.WriteString("  |\n  | Possible fixes:\n")
	}
	for _, fix := range hint.Fixes {
		fmt.Fprintf(&b, "  |   %s %s\n", icons.Bullet, fix)
	}
	b.WriteByte('\n')
	return b.String()
}
