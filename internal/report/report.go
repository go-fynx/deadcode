package report

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-fynx/deadcode/internal/analyzer"
	"github.com/go-fynx/deadcode/internal/color"
	"github.com/go-fynx/deadcode/internal/config"
)

type jsonEntry struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	File       string `json:"file"`
	Line       int    `json:"line"`
	Package    string `json:"package"`
	Receiver   string `json:"receiver,omitempty"`
	Confidence string `json:"confidence"`
	IsExported bool   `json:"is_exported"`
}

type jsonReport struct {
	DeadCode []jsonEntry `json:"dead_code"`
	Summary  jsonSummary `json:"summary"`
}

type jsonSummary struct {
	TotalFunctions int      `json:"total_functions"`
	Reachable      int      `json:"reachable_functions"`
	DeadFunctions  int      `json:"dead_functions"`
	DeadVars       int      `json:"dead_vars"`
	DeadConsts     int      `json:"dead_consts"`
	DeadTypes      int      `json:"dead_types"`
	Skipped        int      `json:"skipped_files"`
	DeadFiles      []string `json:"dead_files,omitempty"`
}

// Write renders the analysis result to w in the format specified by cfg.
func Write(w io.Writer, result *analyzer.Result, cfg *config.Config) error {
	switch cfg.Format {
	case config.OutputJSON:
		return writeJSON(w, result)
	case config.OutputPatch:
		return writePatch(w, result)
	default:
		return writeText(w, result)
	}
}

func writeText(w io.Writer, result *analyzer.Result) error {
	if len(result.DeadCode) == 0 {
		fmt.Fprintln(w, color.BoldGreen("No dead code found."))
		writeSummary(w, result.Summary)
		return nil
	}

	sorted := make([]analyzer.DeadCode, len(result.DeadCode))
	copy(sorted, result.DeadCode)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Confidence != sorted[j].Confidence {
			return sorted[i].Confidence < sorted[j].Confidence
		}
		if sorted[i].File != sorted[j].File {
			return sorted[i].File < sorted[j].File
		}
		return sorted[i].Line < sorted[j].Line
	})

	fmt.Fprintln(w, color.BoldRed("Dead code detected:"))
	fmt.Fprintln(w, color.Separator(80))

	for _, item := range sorted {
		relFile := relativePath(item.File)
		desc := formatDeclDesc(item)
		badge := confidenceBadge(item.Confidence)
		location := color.Cyan(fmt.Sprintf("%s:%d", relFile, item.Line))
		kindLabel := color.Dim(string(item.Kind))

		fmt.Fprintf(w, "%s %s %s %s %s\n",
			badge, location, color.Dim("→"), kindLabel, color.BoldWhite(desc))
	}

	fmt.Fprintln(w, color.Separator(80))

	if len(result.Summary.DeadFiles) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, color.BoldYellow("Fully dead files (all declarations unreachable):"))
		for _, f := range result.Summary.DeadFiles {
			fmt.Fprintf(w, "  %s %s\n", color.Red("●"), color.Yellow(relativePath(f)))
		}
	}

	writeSummary(w, result.Summary)
	return nil
}

func formatDeclDesc(item analyzer.DeadCode) string {
	if item.Kind == analyzer.KindFunc && item.Receiver != "" {
		return fmt.Sprintf("(%s).%s", item.Receiver, item.Name)
	}
	return item.Name
}

func confidenceBadge(confidence config.Confidence) string {
	switch confidence {
	case config.ConfidenceHigh:
		return color.HighBadge("HIGH")
	case config.ConfidenceMedium:
		return color.MediumBadge("MEDIUM")
	default:
		return "[UNKNOWN]"
	}
}

func writeJSON(w io.Writer, result *analyzer.Result) error {
	entries := make([]jsonEntry, 0, len(result.DeadCode))
	for _, item := range result.DeadCode {
		entries = append(entries, jsonEntry{
			Name:       item.Name,
			Kind:       string(item.Kind),
			File:       relativePath(item.File),
			Line:       item.Line,
			Package:    item.Package,
			Receiver:   item.Receiver,
			Confidence: item.Confidence.String(),
			IsExported: item.IsExported,
		})
	}

	deadFiles := make([]string, 0, len(result.Summary.DeadFiles))
	for _, f := range result.Summary.DeadFiles {
		deadFiles = append(deadFiles, relativePath(f))
	}

	report := jsonReport{
		DeadCode: entries,
		Summary: jsonSummary{
			TotalFunctions: result.Summary.TotalFunctions,
			Reachable:      result.Summary.ReachableFunctions,
			DeadFunctions:  result.Summary.DeadFunctions,
			DeadVars:       result.Summary.DeadVars,
			DeadConsts:     result.Summary.DeadConsts,
			DeadTypes:      result.Summary.DeadTypes,
			Skipped:        result.Summary.SkippedFiles,
			DeadFiles:      deadFiles,
		},
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

func writePatch(w io.Writer, result *analyzer.Result) error {
	fmt.Fprintln(w, color.Dim("# Dead code locations (patch-style reference)"))
	fmt.Fprintln(w, color.Dim("# Use --fix or --fix-dry-run to generate actual file modifications"))
	fmt.Fprintln(w)

	byFile := groupByFile(result.DeadCode)
	files := make([]string, 0, len(byFile))
	for f := range byFile {
		files = append(files, f)
	}
	sort.Strings(files)

	for _, file := range files {
		items := byFile[file]
		relFile := relativePath(file)
		fmt.Fprintf(w, "%s\n", color.Red("--- a/"+relFile))
		fmt.Fprintf(w, "%s\n", color.Green("+++ b/"+relFile))

		for _, item := range items {
			desc := formatDeclDesc(item)
			fmt.Fprintf(w, "%s\n", color.Cyan(fmt.Sprintf("@@ -%d [%s] %s %s @@", item.Line, item.Confidence, item.Kind, desc)))
			fmt.Fprintf(w, "%s\n", color.Red(fmt.Sprintf("-// dead code: remove %s %s", item.Kind, desc)))
		}
		fmt.Fprintln(w)
	}

	return nil
}

func writeSummary(w io.Writer, summary analyzer.Summary) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, color.BoldBlue("Summary:"))
	fmt.Fprintf(w, "  Total functions:     %s\n", color.Bold(fmt.Sprintf("%d", summary.TotalFunctions)))
	fmt.Fprintf(w, "  Reachable:           %s\n", color.Green(fmt.Sprintf("%d", summary.ReachableFunctions)))
	fmt.Fprintf(w, "  Dead functions:      %s\n", colorDeadCount(summary.DeadFunctions))
	if summary.DeadVars > 0 {
		fmt.Fprintf(w, "  Dead vars:           %s\n", color.Red(fmt.Sprintf("%d", summary.DeadVars)))
	}
	if summary.DeadConsts > 0 {
		fmt.Fprintf(w, "  Dead consts:         %s\n", color.Red(fmt.Sprintf("%d", summary.DeadConsts)))
	}
	if summary.DeadTypes > 0 {
		fmt.Fprintf(w, "  Dead types:          %s\n", color.Red(fmt.Sprintf("%d", summary.DeadTypes)))
	}
	fmt.Fprintf(w, "  Skipped files:       %s\n", color.Gray(fmt.Sprintf("%d", summary.SkippedFiles)))
	if len(summary.DeadFiles) > 0 {
		fmt.Fprintf(w, "  Fully dead files:    %s\n", color.Red(fmt.Sprintf("%d", len(summary.DeadFiles))))
	}
}

func colorDeadCount(count int) string {
	s := fmt.Sprintf("%d", count)
	if count == 0 {
		return color.Green(s)
	}
	return color.Red(s)
}

func groupByFile(items []analyzer.DeadCode) map[string][]analyzer.DeadCode {
	m := make(map[string][]analyzer.DeadCode)
	for _, item := range items {
		m[item.File] = append(m[item.File], item)
	}
	return m
}

func relativePath(absPath string) string {
	rel, err := filepath.Rel(".", absPath)
	if err != nil {
		return absPath
	}
	return rel
}

// FormatFileList prints the list of dead code items for a specific file.
func FormatFileList(items []analyzer.DeadCode) string {
	var sb strings.Builder
	for _, item := range items {
		desc := formatDeclDesc(item)
		badge := confidenceBadge(item.Confidence)
		sb.WriteString(fmt.Sprintf("    %s %s %s (line %d)\n", badge, color.Dim(string(item.Kind)), color.BoldWhite(desc), item.Line))
	}
	return sb.String()
}
