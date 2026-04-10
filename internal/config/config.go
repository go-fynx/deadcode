package config

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

// Confidence represents how confident we are that code is truly dead.
type Confidence int

const (
	ConfidenceHigh   Confidence = iota // Unexported + unreachable
	ConfidenceMedium                   // Exported but unused internally
)

func (c Confidence) String() string {
	switch c {
	case ConfidenceHigh:
		return "HIGH"
	case ConfidenceMedium:
		return "MEDIUM"
	default:
		return "UNKNOWN"
	}
}

// ShouldInclude reports whether an item at the given confidence level should
// appear in results or be eligible for removal at the configured level.
//
//   - "high"          → HIGH only (unexported, unreachable)
//   - "medium" / "all" → HIGH + MEDIUM (exported but unused internally included)
func ShouldInclude(confidence Confidence, level string) bool {
	switch level {
	case "high":
		return confidence == ConfidenceHigh
	default:
		return true
	}
}

// OutputFormat controls how results are rendered.
type OutputFormat int

const (
	OutputText  OutputFormat = iota
	OutputJSON               // Machine-readable JSON
	OutputPatch              // Unified diff patch
)

// Config holds all CLI flags and derived settings.
type Config struct {
	Patterns        []string     // Package patterns to analyze (e.g., "./...")
	EntryDir        string       // Directory containing main packages (e.g., "./cmd")
	OutputJSON      bool         // Emit JSON output
	OutputPatch     bool         // Generate unified diff patch
	ConfidenceLevel string       // Filter: "high", "medium", or "all"
	IgnorePatterns  []string     // Glob patterns for files/packages to skip
	Fix             bool         // Apply deletions
	FixDryRun       bool         // Preview deletions without writing
	FixFile         string       // Specific file to fix (optional; prompts if empty)
	Backup          bool         // Create .bak files before modifying
	SkipTests       bool         // Exclude _test.go files from analysis
	SkipGenerated   bool         // Exclude files with "Code generated" header
	Verbose         bool         // Print progress information
	MinConfidence   Confidence   // Derived from ConfidenceLevel
	Format          OutputFormat // Derived from flags
}

// Parse reads CLI flags and returns a validated Config.
func Parse() (*Config, error) {
	cfg := &Config{}

	flag.StringVar(&cfg.EntryDir, "entry", "", "directory containing main packages")
	flag.BoolVar(&cfg.OutputJSON, "json", false, "output results as JSON")
	flag.BoolVar(&cfg.OutputPatch, "output-patch", false, "generate unified diff patch")

	flag.StringVar(&cfg.ConfidenceLevel, "confidence", "all", "filter by confidence: high, medium, all")

	var ignoreRaw string
	flag.StringVar(&ignoreRaw, "ignore", "", "comma-separated glob patterns to ignore")

	flag.BoolVar(&cfg.Fix, "fix", false, "remove dead code matching --confidence level (default: high only)")
	flag.BoolVar(&cfg.FixDryRun, "fix-dry-run", false, "preview changes without writing")
	flag.StringVar(&cfg.FixFile, "fix-file", "", "specific file to remove dead code from")
	flag.BoolVar(&cfg.Backup, "backup", false, "create .bak files before modifying")
	flag.BoolVar(&cfg.SkipTests, "skip-tests", true, "skip test files")
	flag.BoolVar(&cfg.SkipGenerated, "skip-generated", true, "skip generated files")
	flag.BoolVar(&cfg.Verbose, "verbose", false, "print extra progress details (package names, ignored paths)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: deadcode [flags] [packages]\n\n")
		fmt.Fprintf(os.Stderr, "Detect and optionally remove dead code across a Go monorepo.\n\n")
		fmt.Fprintf(os.Stderr, "Examples:\n")
		fmt.Fprintf(os.Stderr, "  deadcode ./...\n")
		fmt.Fprintf(os.Stderr, "  deadcode --entry=./cmd --json ./...\n")
		fmt.Fprintf(os.Stderr, "  deadcode --fix --backup ./...\n")
		fmt.Fprintf(os.Stderr, "  deadcode --fix --fix-file=internal/helper/helper.go ./...\n")
		fmt.Fprintf(os.Stderr, "  deadcode --fix-dry-run --confidence=high ./...\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	cfg.Patterns = flag.Args()
	if len(cfg.Patterns) == 0 {
		cfg.Patterns = []string{"./..."}
	}

	if ignoreRaw != "" {
		cfg.IgnorePatterns = strings.Split(ignoreRaw, ",")
		for i := range cfg.IgnorePatterns {
			cfg.IgnorePatterns[i] = strings.TrimSpace(cfg.IgnorePatterns[i])
		}
	}

	switch strings.ToLower(cfg.ConfidenceLevel) {
	case "high":
		cfg.MinConfidence = ConfidenceHigh
	case "medium":
		cfg.MinConfidence = ConfidenceMedium
	case "all":
		cfg.MinConfidence = ConfidenceMedium
	default:
		return nil, fmt.Errorf("invalid confidence level %q: must be high, medium, or all", cfg.ConfidenceLevel)
	}

	cfg.Format = OutputText
	if cfg.OutputJSON {
		cfg.Format = OutputJSON
	}
	if cfg.OutputPatch {
		cfg.Format = OutputPatch
	}

	if cfg.Fix && cfg.FixDryRun {
		return nil, fmt.Errorf("--fix and --fix-dry-run are mutually exclusive")
	}

	return cfg, nil
}
