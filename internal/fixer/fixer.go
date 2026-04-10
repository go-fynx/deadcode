package fixer

import (
	"bufio"
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-fynx/deadcode/internal/analyzer"
	"github.com/go-fynx/deadcode/internal/color"
	"github.com/go-fynx/deadcode/internal/config"
	"github.com/go-fynx/deadcode/internal/logger"
)

// FixResult records what the fixer did.
type FixResult struct {
	Modified     []string // Files that were modified
	Deleted      []string // Files that were entirely removed
	BackupFiles  []string // .bak files created
	ItemsRemoved int
	Errors       []string
}

// Fixer removes dead code from source files using the Go AST.
type Fixer struct {
	cfg *config.Config
}

// New creates a new Fixer with the given configuration.
func New(cfg *config.Config) *Fixer {
	return &Fixer{cfg: cfg}
}

// Fix processes the analysis result and removes dead code matching the
// configured confidence level. When FixFile is set, only that file is
// processed; otherwise the user is prompted to select which file(s) to fix.
func (fixer *Fixer) Fix(result *analyzer.Result) (*FixResult, error) {
	if err := fixer.checkGitStatus(); err != nil {
		return nil, err
	}

	fixable := filterFixable(result.DeadCode, fixer.cfg.ConfidenceLevel)
	if len(fixable) == 0 {
		return &FixResult{}, nil
	}

	byFile := groupByFile(fixable)

	fixResult := &FixResult{}

	deadFileSet := make(map[string]bool)
	for _, f := range result.Summary.DeadFiles {
		deadFileSet[f] = true
	}

	var targetFiles []string

	if fixer.cfg.FixFile != "" {
		absFixFile, err := resolveFixFile(fixer.cfg.FixFile, byFile)
		if err != nil {
			return nil, err
		}
		targetFiles = []string{absFixFile}
	} else {
		selected, err := promptFileSelection(byFile)
		if err != nil {
			return nil, err
		}
		if len(selected) == 0 {
			logger.Warn("%s", color.Yellow("No files selected — aborting fix."))
			return &FixResult{}, nil
		}
		targetFiles = selected
	}

	for _, file := range targetFiles {
		items, ok := byFile[file]
		if !ok {
			continue
		}

		if deadFileSet[file] && allMatchConfidence(items, fixer.cfg.ConfidenceLevel) {
			if err := fixer.handleDeadFile(file, fixResult); err != nil {
				fixResult.Errors = append(fixResult.Errors, fmt.Sprintf("%s: %v", file, err))
			}
			continue
		}

		if err := fixer.removeFromFile(file, items, fixResult); err != nil {
			fixResult.Errors = append(fixResult.Errors, fmt.Sprintf("%s: %v", file, err))
		}
	}

	return fixResult, nil
}

func resolveFixFile(fixFile string, byFile map[string][]analyzer.DeadCode) (string, error) {
	absPath, err := filepath.Abs(fixFile)
	if err != nil {
		absPath = fixFile
	}
	if _, ok := byFile[absPath]; ok {
		return absPath, nil
	}

	for file := range byFile {
		rel, relErr := filepath.Rel(".", file)
		if relErr == nil && rel == fixFile {
			return file, nil
		}
		if filepath.Base(file) == fixFile {
			return file, nil
		}
	}

	logger.Fail("File %s has no fixable dead code at configured confidence level.", color.BoldWhite(fixFile))
	logger.Blank()
	logger.Line(color.Bold("Files with fixable dead code:"))
	for file := range byFile {
		logger.Linef("  %s %s", color.Cyan("•"), color.Yellow(relativePath(file)))
	}

	return "", fmt.Errorf("file %q not found in dead code results", fixFile)
}

func promptFileSelection(byFile map[string][]analyzer.DeadCode) ([]string, error) {
	sortedFiles := make([]string, 0, len(byFile))
	for f := range byFile {
		sortedFiles = append(sortedFiles, f)
	}
	sort.Strings(sortedFiles)

	logger.Blank()
	logger.Section("Files with dead code to fix:")
	logger.Separator(60)

	for i, file := range sortedFiles {
		items := byFile[file]
		relFile := relativePath(file)

		logger.Linef("  %s  %s  %s",
			color.BoldCyan(fmt.Sprintf("[%d]", i+1)),
			color.Yellow(relFile),
			color.Dim(fmt.Sprintf("(%d dead item(s))", len(items))))

		for _, item := range items {
			desc := item.Name
			if item.Kind == analyzer.KindFunc && item.Receiver != "" {
				desc = fmt.Sprintf("(%s).%s", item.Receiver, item.Name)
			}
			logger.Linef("        %s %s %s (line %d)",
				color.Red("−"), color.Dim(string(item.Kind)), color.Gray(desc), item.Line)
		}
	}

	logger.Separator(60)
	fmt.Fprintf(os.Stderr, "\n%s Enter file numbers to fix (e.g. %s), or %s for all: ",
		color.Bold("→"),
		color.Cyan("1,3,5"),
		color.Cyan("all"))

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("reading input: %w", err)
	}

	input = strings.TrimSpace(input)
	if input == "" {
		return nil, nil
	}

	if strings.EqualFold(input, "all") {
		return sortedFiles, nil
	}

	var selected []string
	parts := strings.Split(input, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		var idx int
		if _, scanErr := fmt.Sscanf(part, "%d", &idx); scanErr != nil {
			logger.Warn("Skipping invalid input: %s", part)
			continue
		}
		if idx < 1 || idx > len(sortedFiles) {
			logger.Warn("Skipping out-of-range: %d", idx)
			continue
		}
		selected = append(selected, sortedFiles[idx-1])
	}

	return selected, nil
}

func filterFixable(items []analyzer.DeadCode, confidenceLevel string) []analyzer.DeadCode {
	var filtered []analyzer.DeadCode
	for _, item := range items {
		if !config.ShouldInclude(item.Confidence, confidenceLevel) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func allMatchConfidence(items []analyzer.DeadCode, confidenceLevel string) bool {
	for _, item := range items {
		if !config.ShouldInclude(item.Confidence, confidenceLevel) {
			return false
		}
	}
	return true
}

func groupByFile(items []analyzer.DeadCode) map[string][]analyzer.DeadCode {
	m := make(map[string][]analyzer.DeadCode)
	for _, item := range items {
		m[item.File] = append(m[item.File], item)
	}
	return m
}

// removeFromFile parses a single Go file, removes the specified dead code items
// (functions, vars, consts, types) along with their doc comments, and writes
// the modified source back.
func (fixer *Fixer) removeFromFile(filename string, deadItems []analyzer.DeadCode, fixResult *FixResult) error {
	fset := token.NewFileSet()
	astFile, err := parser.ParseFile(fset, filename, nil, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", filename, err)
	}

	deadFuncs := make(map[string]bool)
	deadDecls := make(map[string]analyzer.DeclKind)
	for _, item := range deadItems {
		key := declKey(item.Name, item.Line)
		if item.Kind == analyzer.KindFunc {
			deadFuncs[key] = true
		} else {
			deadDecls[key] = item.Kind
		}
	}

	removedComments := make(map[*ast.CommentGroup]bool)
	var kept []ast.Decl
	removed := 0

	for _, decl := range astFile.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			pos := fset.Position(d.Pos())
			name := d.Name.Name
			key := declKey(name, pos.Line)

			if deadFuncs[key] {
				removed++
				if d.Doc != nil {
					removedComments[d.Doc] = true
				}
				fixer.logRemoval(filename, pos.Line, analyzer.KindFunc, funcDeclDesc(d))
				continue
			}

		case *ast.GenDecl:
			if d.Tok == token.VAR || d.Tok == token.CONST || d.Tok == token.TYPE {
				keptSpecs, removedCount := fixer.filterGenDeclSpecs(fset, filename, d, deadDecls, removedComments)

				if removedCount > 0 && len(keptSpecs) == 0 {
					removed += removedCount
					if d.Doc != nil {
						removedComments[d.Doc] = true
					}
					continue
				}

				if removedCount > 0 {
					removed += removedCount
					d.Specs = keptSpecs
				}
			}
		}

		kept = append(kept, decl)
	}

	if removed == 0 {
		return nil
	}

	astFile.Decls = kept

	if len(removedComments) > 0 {
		var keptComments []*ast.CommentGroup
		for _, cg := range astFile.Comments {
			if !removedComments[cg] {
				keptComments = append(keptComments, cg)
			}
		}
		astFile.Comments = keptComments
	}

	var buf bytes.Buffer
	if err := format.Node(&buf, fset, astFile); err != nil {
		return fmt.Errorf("formatting %s: %w", filename, err)
	}

	if fixer.cfg.FixDryRun {
		fixResult.ItemsRemoved += removed
		fixResult.Modified = append(fixResult.Modified, filename)
		return nil
	}

	if fixer.cfg.Backup {
		bakFile := filename + ".bak"
		original, readErr := os.ReadFile(filename)
		if readErr != nil {
			return fmt.Errorf("reading %s for backup: %w", filename, readErr)
		}
		if writeErr := os.WriteFile(bakFile, original, 0644); writeErr != nil {
			return fmt.Errorf("creating backup %s: %w", bakFile, writeErr)
		}
		fixResult.BackupFiles = append(fixResult.BackupFiles, bakFile)
	}

	if err := os.WriteFile(filename, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("writing %s: %w", filename, err)
	}

	fixResult.ItemsRemoved += removed
	fixResult.Modified = append(fixResult.Modified, filename)
	return nil
}

// filterGenDeclSpecs iterates over the specs in a GenDecl (var/const/type block)
// and returns the specs to keep plus how many were removed.
func (fixer *Fixer) filterGenDeclSpecs(
	fset *token.FileSet,
	filename string,
	genDecl *ast.GenDecl,
	deadDecls map[string]analyzer.DeclKind,
	removedComments map[*ast.CommentGroup]bool,
) (keptSpecs []ast.Spec, removedCount int) {
	kind := tokenToKind(genDecl.Tok)

	for _, spec := range genDecl.Specs {
		switch s := spec.(type) {
		case *ast.ValueSpec:
			var keptNames []*ast.Ident
			var keptValues []ast.Expr
			anyRemoved := false

			for idx, name := range s.Names {
				pos := fset.Position(name.Pos())
				key := declKey(name.Name, pos.Line)

				if _, isDead := deadDecls[key]; isDead {
					removedCount++
					anyRemoved = true
					fixer.logRemoval(filename, pos.Line, kind, name.Name)
					continue
				}
				keptNames = append(keptNames, name)
				if s.Values != nil && idx < len(s.Values) {
					keptValues = append(keptValues, s.Values[idx])
				}
			}

			if len(keptNames) == 0 {
				if s.Doc != nil {
					removedComments[s.Doc] = true
				}
				continue
			}
			if anyRemoved {
				s.Names = keptNames
				s.Values = keptValues
			}
			keptSpecs = append(keptSpecs, s)

		case *ast.TypeSpec:
			pos := fset.Position(s.Name.Pos())
			key := declKey(s.Name.Name, pos.Line)
			if _, isDead := deadDecls[key]; isDead {
				removedCount++
				if s.Doc != nil {
					removedComments[s.Doc] = true
				}
				fixer.logRemoval(filename, pos.Line, KindType, s.Name.Name)
				continue
			}
			keptSpecs = append(keptSpecs, s)
		}
	}

	return keptSpecs, removedCount
}

// KindType is re-exported for use inside filterGenDeclSpecs without import cycle.
const KindType = analyzer.KindType

func tokenToKind(tok token.Token) analyzer.DeclKind {
	switch tok {
	case token.VAR:
		return analyzer.KindVar
	case token.CONST:
		return analyzer.KindConst
	case token.TYPE:
		return analyzer.KindType
	default:
		return "unknown"
	}
}

func funcDeclDesc(funcDecl *ast.FuncDecl) string {
	name := funcDecl.Name.Name
	if funcDecl.Recv != nil && len(funcDecl.Recv.List) > 0 {
		recv := receiverTypeName(funcDecl)
		return fmt.Sprintf("(%s).%s", recv, name)
	}
	return name
}

func (fixer *Fixer) logRemoval(filename string, line int, kind analyzer.DeclKind, desc string) {
	relPath := relativePath(filename)
	if fixer.cfg.FixDryRun {
		logger.Linef("  %s %s:%d → %s %s",
			color.Yellow("[DRY-RUN REMOVE]"),
			color.Cyan(relPath), line,
			color.Dim(string(kind)),
			color.BoldWhite(desc))
	} else {
		logger.Linef("  %s %s:%d → %s %s",
			color.Red("[REMOVE]"),
			color.Cyan(relPath), line,
			color.Dim(string(kind)),
			color.BoldWhite(desc))
	}
}

func receiverTypeName(funcDecl *ast.FuncDecl) string {
	if funcDecl.Recv == nil || len(funcDecl.Recv.List) == 0 {
		return ""
	}
	expr := funcDecl.Recv.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		if ident, ok := star.X.(*ast.Ident); ok {
			return "*" + ident.Name
		}
	}
	if ident, ok := expr.(*ast.Ident); ok {
		return ident.Name
	}
	return "?"
}

// handleDeadFile deletes or backs up a fully dead file.
func (fixer *Fixer) handleDeadFile(filename string, fixResult *FixResult) error {
	relPath := relativePath(filename)

	if fixer.cfg.FixDryRun {
		logger.Linef("  %s %s %s",
			color.BoldYellow("[DRY-RUN DELETE]"),
			color.Yellow(relPath),
			color.Dim("(all declarations dead)"))
		fixResult.Deleted = append(fixResult.Deleted, filename)
		return nil
	}

	if fixer.cfg.Backup {
		bakFile := filename + ".bak"
		original, err := os.ReadFile(filename)
		if err != nil {
			return fmt.Errorf("reading %s for backup: %w", filename, err)
		}
		if err := os.WriteFile(bakFile, original, 0644); err != nil {
			return fmt.Errorf("creating backup %s: %w", bakFile, err)
		}
		fixResult.BackupFiles = append(fixResult.BackupFiles, bakFile)
	}

	if err := os.Remove(filename); err != nil {
		return fmt.Errorf("removing %s: %w", filename, err)
	}

	logger.Linef("  %s %s", color.BoldRed("[DELETED]"), color.Red(relPath))
	fixResult.Deleted = append(fixResult.Deleted, filename)
	return nil
}

func (fixer *Fixer) checkGitStatus() error {
	cmd := exec.Command("git", "status", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	if len(bytes.TrimSpace(output)) > 0 {
		logger.Warn("%s", color.Yellow("Git working tree has uncommitted changes."))
		logger.Warn("%s", color.Dim("Consider committing or stashing before running --fix."))
		logger.Blank()
	}

	return nil
}

func declKey(name string, line int) string {
	return fmt.Sprintf("%s@%d", name, line)
}

func relativePath(absPath string) string {
	rel, err := filepath.Rel(".", absPath)
	if err != nil {
		return absPath
	}
	return rel
}

// PrintFixResult displays the fix summary with colors.
func PrintFixResult(result *FixResult) {
	if result.ItemsRemoved == 0 && len(result.Deleted) == 0 {
		logger.Success("%s", color.Green("No changes made."))
		return
	}

	logger.Blank()
	logger.Separator(60)
	logger.Section("Fix Summary:")
	logger.KeyValue("Items removed:", color.BoldRed(fmt.Sprintf("%d", result.ItemsRemoved)))
	logger.KeyValue("Files modified:", color.Yellow(fmt.Sprintf("%d", len(result.Modified))))
	logger.KeyValue("Files deleted:", color.Red(fmt.Sprintf("%d", len(result.Deleted))))

	if len(result.BackupFiles) > 0 {
		logger.KeyValue("Backups created:", color.Cyan(fmt.Sprintf("%d", len(result.BackupFiles))))
		for _, bak := range result.BackupFiles {
			logger.Linef("    %s", color.Gray(relativePath(bak)))
		}
	}

	if len(result.Errors) > 0 {
		logger.Blank()
		logger.Line(color.BoldRed("Errors:"))
		for _, e := range result.Errors {
			logger.Fail("%s", e)
		}
	}
}
