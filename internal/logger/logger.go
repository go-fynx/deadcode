package logger

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/go-fynx/deadcode/internal/color"
)

var std = New(os.Stderr)

// Logger provides structured, colorized CLI output with spinner support.
type Logger struct {
	mu      sync.Mutex
	w       io.Writer
	spinner *spinnerState
}

type spinnerState struct {
	message string
	running bool
	done    chan struct{}
}

// New creates a Logger that writes to the given writer.
func New(w io.Writer) *Logger {
	return &Logger{w: w}
}

// --- Semantic output methods ---

// Header prints a bold blue title with a separator line beneath it.
func (logger *Logger) Header(title, subtitle string) {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	fmt.Fprintln(logger.w, color.BoldBlue(title)+" "+color.Dim(subtitle))
	fmt.Fprintln(logger.w, color.Separator(50))
}

// Section prints a bold blue section heading.
func (logger *Logger) Section(heading string) {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	fmt.Fprintln(logger.w, color.BoldBlue(heading))
}

// Separator prints a dim horizontal line.
func (logger *Logger) Separator(width int) {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	fmt.Fprintln(logger.w, color.Separator(width))
}

// Blank prints an empty line.
func (logger *Logger) Blank() {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	fmt.Fprintln(logger.w)
}

// Success prints a green check mark with a message.
func (logger *Logger) Success(format string, args ...any) {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(logger.w, "  %s %s\n", color.Green("\u2713"), msg)
}

// Fail prints a red cross with a message.
func (logger *Logger) Fail(format string, args ...any) {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(logger.w, "  %s %s\n", color.Red("\u2717"), msg)
}

// Warn prints a yellow exclamation mark with a message.
func (logger *Logger) Warn(format string, args ...any) {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(logger.w, "  %s %s\n", color.Yellow("!"), msg)
}

// Info prints a blue arrow with a message.
func (logger *Logger) Info(format string, args ...any) {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(logger.w, "  %s %s\n", color.Blue("\u2192"), msg)
}

// Error prints a bold red prefixed error.
func (logger *Logger) Error(prefix string, err error) {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	fmt.Fprintf(logger.w, "%s %v\n", color.BoldRed(prefix), err)
}

// Linef prints a formatted line (no prefix icon).
func (logger *Logger) Linef(format string, args ...any) {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	fmt.Fprintf(logger.w, format+"\n", args...)
}

// Line prints a plain line (no prefix icon).
func (logger *Logger) Line(text string) {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	fmt.Fprintln(logger.w, text)
}

// KeyValue prints a labeled value with consistent alignment.
func (logger *Logger) KeyValue(label, value string) {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	fmt.Fprintf(logger.w, "  %-20s %s\n", label, value)
}

// Banner prints a highlighted section header (e.g. "--- Dry Run ---").
func (logger *Logger) Banner(text string, colorFn func(string) string) {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	fmt.Fprintln(logger.w)
	fmt.Fprintln(logger.w, colorFn("--- "+text+" ---"))
}

// --- Spinner methods ---

var frames = []string{"\u280b", "\u2819", "\u2839", "\u2838", "\u283c", "\u2834", "\u2826", "\u2827", "\u2807", "\u280f"}

// SpinStart begins a spinner animation with the given message.
func (logger *Logger) SpinStart(message string) {
	logger.mu.Lock()
	if logger.spinner != nil && logger.spinner.running {
		logger.mu.Unlock()
		logger.SpinUpdate(message)
		return
	}
	logger.spinner = &spinnerState{
		message: message,
		running: true,
		done:    make(chan struct{}),
	}
	spinner := logger.spinner
	logger.mu.Unlock()

	go logger.animate(spinner)
}

// SpinUpdate changes the spinner message while it is running.
func (logger *Logger) SpinUpdate(message string) {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	if logger.spinner != nil {
		logger.spinner.message = message
	}
}

// SpinStop halts the spinner and prints a success message.
func (logger *Logger) SpinStop(format string, args ...any) {
	logger.mu.Lock()
	spinner := logger.spinner
	if spinner == nil || !spinner.running {
		logger.mu.Unlock()
		return
	}
	spinner.running = false
	logger.mu.Unlock()

	<-spinner.done
	clearLine(logger.w)
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(logger.w, "  %s %s\n", color.Green("\u2713"), msg)
}

// SpinFail halts the spinner and prints a failure message.
func (logger *Logger) SpinFail(format string, args ...any) {
	logger.mu.Lock()
	spinner := logger.spinner
	if spinner == nil || !spinner.running {
		logger.mu.Unlock()
		return
	}
	spinner.running = false
	logger.mu.Unlock()

	<-spinner.done
	clearLine(logger.w)
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(logger.w, "  %s %s\n", color.Red("\u2717"), msg)
}

func (logger *Logger) animate(spinner *spinnerState) {
	defer close(spinner.done)
	i := 0
	for {
		logger.mu.Lock()
		if !spinner.running {
			logger.mu.Unlock()
			return
		}
		msg := spinner.message
		logger.mu.Unlock()

		frame := color.Cyan(frames[i%len(frames)])
		clearLine(logger.w)
		fmt.Fprintf(logger.w, "  %s %s", frame, msg)

		i++
		time.Sleep(80 * time.Millisecond)
	}
}

func clearLine(w io.Writer) {
	fmt.Fprintf(w, "\r\033[K")
}

// --- Package-level convenience functions (delegate to std) ---

// Header prints a bold blue title with a separator line beneath it.
func Header(title, subtitle string) { std.Header(title, subtitle) }

// Section prints a bold blue section heading.
func Section(heading string) { std.Section(heading) }

// Separator prints a dim horizontal line.
func Separator(width int) { std.Separator(width) }

// Blank prints an empty line.
func Blank() { std.Blank() }

// Success prints a green check mark with a message.
func Success(format string, args ...any) { std.Success(format, args...) }

// Fail prints a red cross with a message.
func Fail(format string, args ...any) { std.Fail(format, args...) }

// Warn prints a yellow exclamation mark with a message.
func Warn(format string, args ...any) { std.Warn(format, args...) }

// Info prints a blue arrow with a message.
func Info(format string, args ...any) { std.Info(format, args...) }

// Error prints a bold red prefixed error.
func Error(prefix string, err error) { std.Error(prefix, err) }

// Linef prints a formatted line.
func Linef(format string, args ...any) { std.Linef(format, args...) }

// Line prints a plain line.
func Line(text string) { std.Line(text) }

// KeyValue prints a labeled value.
func KeyValue(label, value string) { std.KeyValue(label, value) }

// Banner prints a highlighted section header.
func Banner(text string, colorFn func(string) string) { std.Banner(text, colorFn) }

// SpinStart begins a spinner animation.
func SpinStart(message string) { std.SpinStart(message) }

// SpinUpdate changes the spinner message.
func SpinUpdate(message string) { std.SpinUpdate(message) }

// SpinStop halts the spinner with a success message.
func SpinStop(format string, args ...any) { std.SpinStop(format, args...) }

// SpinFail halts the spinner with a failure message.
func SpinFail(format string, args ...any) { std.SpinFail(format, args...) }
