package color

import (
	"fmt"
	"os"
	"runtime"
	"strings"
)

var enabled = detectColorSupport()

func detectColorSupport() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("FORCE_COLOR") != "" {
		return true
	}
	if runtime.GOOS == "windows" {
		return os.Getenv("TERM") == "xterm" || os.Getenv("WT_SESSION") != ""
	}
	return isTerminal(os.Stdout)
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

const (
	reset   = "\033[0m"
	bold    = "\033[1m"
	dim     = "\033[2m"
	red     = "\033[31m"
	green   = "\033[32m"
	yellow  = "\033[33m"
	blue    = "\033[34m"
	magenta = "\033[35m"
	cyan    = "\033[36m"
	white   = "\033[37m"
	gray    = "\033[90m"

	bgRed    = "\033[41m"
	bgGreen  = "\033[42m"
	bgYellow = "\033[43m"
)

func wrap(code, text string) string {
	if !enabled {
		return text
	}
	return code + text + reset
}

// Red applies red color to text.
func Red(text string) string { return wrap(red, text) }

// Green applies green color to text.
func Green(text string) string { return wrap(green, text) }

// Yellow applies yellow color to text.
func Yellow(text string) string { return wrap(yellow, text) }

// Blue applies blue color to text.
func Blue(text string) string { return wrap(blue, text) }

// Magenta applies magenta color to text.
func Magenta(text string) string { return wrap(magenta, text) }

// Cyan applies cyan color to text.
func Cyan(text string) string { return wrap(cyan, text) }

// White applies white color to text.
func White(text string) string { return wrap(white, text) }

// Gray applies gray color to text.
func Gray(text string) string { return wrap(gray, text) }

// Bold applies bold formatting to text.
func Bold(text string) string { return wrap(bold, text) }

// Dim applies dim formatting to text.
func Dim(text string) string { return wrap(dim, text) }

// BoldRed applies bold red to text.
func BoldRed(text string) string { return wrap(bold+red, text) }

// BoldGreen applies bold green to text.
func BoldGreen(text string) string { return wrap(bold+green, text) }

// BoldYellow applies bold yellow to text.
func BoldYellow(text string) string { return wrap(bold+yellow, text) }

// BoldCyan applies bold cyan to text.
func BoldCyan(text string) string { return wrap(bold+cyan, text) }

// BoldBlue applies bold blue to text.
func BoldBlue(text string) string { return wrap(bold+blue, text) }

// BoldWhite applies bold white to text.
func BoldWhite(text string) string { return wrap(bold+white, text) }

// HighBadge returns text with a red background badge for HIGH confidence.
func HighBadge(text string) string { return wrap(bold+bgRed+white, " "+text+" ") }

// MediumBadge returns text with a yellow background badge for MEDIUM confidence.
func MediumBadge(text string) string { return wrap(bold+bgYellow+white, " "+text+" ") }

// SuccessBadge returns text with a green background badge.
func SuccessBadge(text string) string { return wrap(bold+bgGreen+white, " "+text+" ") }

// Separator returns a colored separator line.
func Separator(width int) string {
	return Dim(strings.Repeat("─", width))
}

// Sprintf applies fmt.Sprintf then wraps with color.
func Sprintf(colorFn func(string) string, format string, args ...any) string {
	return colorFn(fmt.Sprintf(format, args...))
}
