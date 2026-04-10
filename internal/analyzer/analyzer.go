package analyzer

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/callgraph/rta"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"

	"github.com/go-fynx/deadcode/internal/config"
	"github.com/go-fynx/deadcode/internal/logger"
)

// DeclKind describes the kind of a dead code declaration.
type DeclKind string

const (
	KindFunc  DeclKind = "func"
	KindVar   DeclKind = "var"
	KindConst DeclKind = "const"
	KindType  DeclKind = "type"
)

// DeadCode represents a single identifier detected as dead (unused) code.
type DeadCode struct {
	Name       string
	Kind       DeclKind
	File       string // Absolute path
	Line       int
	Column     int
	Package    string
	Receiver   string            // Non-empty only for methods
	Confidence config.Confidence // HIGH or MEDIUM
	IsExported bool
}

// Summary holds aggregate statistics for the analysis run.
type Summary struct {
	TotalFunctions     int
	ReachableFunctions int
	DeadFunctions      int
	DeadVars           int
	DeadConsts         int
	DeadTypes          int
	SkippedFiles       int
	DeadFiles          []string // Files where ALL declarations are dead
}

// TotalDead returns the sum of all dead code items.
func (summary Summary) TotalDead() int {
	return summary.DeadFunctions + summary.DeadVars + summary.DeadConsts + summary.DeadTypes
}

// Result bundles dead code list with summary statistics.
type Result struct {
	DeadCode []DeadCode
	Summary  Summary
}

// Analyzer performs whole-program dead code detection using SSA and RTA.
type Analyzer struct {
	cfg     *config.Config
	fset    *token.FileSet
	allPkgs []*packages.Package
	ssaProg *ssa.Program
	ssaPkgs []*ssa.Package
}

// New creates a new Analyzer from the given configuration.
func New(cfg *config.Config) *Analyzer {
	return &Analyzer{cfg: cfg}
}

// Run executes the full analysis pipeline:
// 1. Load packages via go/packages
// 2. Build SSA representation
// 3. Run RTA from every main.main entrypoint
// 4. Diff reachable vs total to find dead code
func (analyzer *Analyzer) Run() (*Result, error) {
	logger.SpinStart("Loading packages...")
	if err := analyzer.loadPackages(); err != nil {
		logger.SpinFail("Failed to load packages")
		return nil, fmt.Errorf("loading packages: %w", err)
	}
	logger.SpinStop("Loaded %d package(s)", len(analyzer.allPkgs))

	logger.SpinStart("Building SSA representation...")
	if err := analyzer.buildSSA(); err != nil {
		logger.SpinFail("Failed to build SSA")
		return nil, fmt.Errorf("building SSA: %w", err)
	}
	logger.SpinStop("SSA built successfully")

	logger.SpinStart("Running reachability analysis (RTA)...")
	reachable, err := analyzer.computeReachable()
	if err != nil {
		logger.SpinFail("Reachability analysis failed")
		return nil, fmt.Errorf("computing reachable set: %w", err)
	}
	logger.SpinStop("RTA complete — %d reachable function(s)", len(reachable))

	logger.SpinStart("Scanning for dead code...")
	result, err := analyzer.findDead(reachable)
	if err != nil {
		logger.SpinFail("Dead code scan failed")
		return nil, err
	}
	logger.SpinStop("Scan complete — %d dead item(s) found", result.Summary.TotalDead())

	logger.Blank()
	return result, nil
}

// loadPackages uses go/packages to load all packages matching the configured patterns.
func (analyzer *Analyzer) loadPackages() error {
	loadCfg := &packages.Config{
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedSyntax |
			packages.NeedTypes |
			packages.NeedTypesInfo |
			packages.NeedDeps |
			packages.NeedImports,
		Tests: !analyzer.cfg.SkipTests,
	}

	pkgs, err := packages.Load(loadCfg, analyzer.cfg.Patterns...)
	if err != nil {
		return fmt.Errorf("packages.Load: %w", err)
	}

	var errs []string
	packages.Visit(pkgs, nil, func(pkg *packages.Package) {
		for _, e := range pkg.Errors {
			errs = append(errs, e.Error())
		}
	})
	if len(errs) > 0 {
		return fmt.Errorf("package errors:\n  %s", strings.Join(errs, "\n  "))
	}

	analyzer.allPkgs = pkgs

	if analyzer.cfg.Verbose {
		for _, pkg := range pkgs {
			logger.Info("loaded: %s", pkg.PkgPath)
		}
	}

	return nil
}

// buildSSA constructs the SSA intermediate representation for all loaded packages.
// SSA (Static Single Assignment) normalises Go code into a form suitable
// for whole-program analyses like RTA.
func (analyzer *Analyzer) buildSSA() error {
	prog, pkgs := ssautil.AllPackages(analyzer.allPkgs, ssa.InstantiateGenerics)
	prog.Build()

	analyzer.fset = analyzer.allPkgs[0].Fset
	analyzer.ssaProg = prog
	analyzer.ssaPkgs = pkgs
	return nil
}

// computeReachable runs Rapid Type Analysis (RTA) from every main.main function
// found under the configured entry directory. RTA resolves interface calls and
// dynamic dispatch to build a sound call graph; the union of all reachable
// functions across all entrypoints forms the global reachable set.
//
// When EntryDir is empty, all main packages in the loaded set are used as roots.
func (analyzer *Analyzer) computeReachable() (map[*ssa.Function]bool, error) {
	var roots []*ssa.Function

	// Resolve the entry directory once. An empty EntryDir means "use all mains".
	useAllMains := analyzer.cfg.EntryDir == ""
	var entryAbs string
	if !useAllMains {
		abs, err := filepath.Abs(analyzer.cfg.EntryDir)
		if err != nil {
			return nil, fmt.Errorf("resolving entry dir %q: %w", analyzer.cfg.EntryDir, err)
		}
		entryAbs = abs
	}

	for _, pkg := range analyzer.ssaPkgs {
		if pkg == nil {
			continue
		}
		if pkg.Pkg.Name() != "main" {
			continue
		}

		if !useAllMains {
			pkgPath := pkg.Pkg.Path()
			pkgAbs, absErr := filepath.Abs(pkgPath)
			if absErr != nil {
				pkgAbs = pkgPath
			}

			// Accept the package if its absolute path starts with the entry dir
			// or its import path contains the entry dir fragment (handles module paths).
			isUnderEntry := strings.HasPrefix(pkgAbs, entryAbs) ||
				strings.Contains(pkgPath, filepath.ToSlash(analyzer.cfg.EntryDir))
			if !isUnderEntry {
				continue
			}
		}

		mainFn := pkg.Func("main")
		if mainFn == nil {
			continue
		}
		roots = append(roots, mainFn)

		if initFn := pkg.Func("init"); initFn != nil {
			roots = append(roots, initFn)
		}

		if analyzer.cfg.Verbose {
			logger.Info("RTA root: %s", pkg.Pkg.Path())
		}
	}

	if len(roots) == 0 {
		if !useAllMains {
			return nil, fmt.Errorf("no main packages found under %q — check the --entry flag", analyzer.cfg.EntryDir)
		}
		return nil, fmt.Errorf("no main packages found in the analyzed set")
	}

	rtaResult := rta.Analyze(roots, true)

	reachable := make(map[*ssa.Function]bool, len(rtaResult.Reachable))
	for fn := range rtaResult.Reachable {
		reachable[fn] = true
	}

	for _, pkg := range analyzer.ssaPkgs {
		if pkg == nil {
			continue
		}
		for _, member := range pkg.Members {
			if fn, ok := member.(*ssa.Function); ok && fn.Name() == "init" {
				reachable[fn] = true
			}
		}
		initFn := pkg.Func("init")
		if initFn != nil {
			reachable[initFn] = true
		}
	}

	return reachable, nil
}

// findDead compares every function in the program against the reachable set
// and classifies unreachable ones by confidence level. It also detects unused
// package-level variables, constants, and types via TypesInfo references.
func (analyzer *Analyzer) findDead(reachable map[*ssa.Function]bool) (*Result, error) {
	var dead []DeadCode
	totalFuncs := 0
	reachableCount := 0

	fileDeclCount := make(map[string]int)
	fileDeadCount := make(map[string]int)
	skippedFiles := make(map[string]bool)

	deadFuncCount := 0
	deadVarCount := 0
	deadConstCount := 0
	deadTypeCount := 0

	// Build the live-type set BEFORE scanning functions/methods so we can
	// skip methods on types that are alive via interface dispatch (DI / port-adapter).
	liveTypes := analyzer.buildLiveTypeSet(reachable)

	// --- Dead functions ---
	for _, pkg := range analyzer.ssaPkgs {
		if pkg == nil {
			continue
		}

		if analyzer.shouldIgnorePackage(pkg.Pkg.Path()) {
			continue
		}

		for _, member := range pkg.Members {
			fn, ok := member.(*ssa.Function)
			if !ok {
				continue
			}

			if fn.Synthetic != "" {
				continue
			}

			pos := analyzer.fset.Position(fn.Pos())
			if !pos.IsValid() {
				continue
			}

			if analyzer.shouldSkipFile(pos.Filename) {
				skippedFiles[pos.Filename] = true
				continue
			}

			if hasKeepAnnotation(fn, analyzer.fset) {
				continue
			}

			totalFuncs++
			fileDeclCount[pos.Filename]++

			if reachable[fn] {
				reachableCount++
				continue
			}

			if analyzer.hasReachableAnon(fn, reachable) {
				reachableCount++
				continue
			}

			isExported := token.IsExported(fn.Name())

			confidence := config.ConfidenceHigh
			if isExported {
				confidence = config.ConfidenceMedium
			}

			if !config.ShouldInclude(confidence, analyzer.cfg.ConfidenceLevel) {
				continue
			}

			receiver := ""
			if fn.Signature.Recv() != nil {
				receiver = fn.Signature.Recv().Type().String()
			}

			dead = append(dead, DeadCode{
				Name:       fn.Name(),
				Kind:       KindFunc,
				File:       pos.Filename,
				Line:       pos.Line,
				Column:     pos.Column,
				Package:    pkg.Pkg.Path(),
				Receiver:   receiver,
				Confidence: confidence,
				IsExported: isExported,
			})
			fileDeadCount[pos.Filename]++
			deadFuncCount++
		}

		// Methods on named types — check both value and pointer receiver method sets.
		// Track visited methods to avoid double-counting (value-receiver methods
		// appear in both value and pointer method sets).
		visitedMethods := make(map[*ssa.Function]bool)

		for _, member := range pkg.Members {
			typ, ok := member.(*ssa.Type)
			if !ok {
				continue
			}

			typeName := typ.Object().(*types.TypeName)

			// If this type is alive (has reachable methods via interface dispatch),
			// all its methods are considered alive — skip the entire type.
			if liveTypes[typeName] {
				for _, methodSet := range []*types.MethodSet{
					analyzer.ssaProg.MethodSets.MethodSet(typ.Type()),
					analyzer.ssaProg.MethodSets.MethodSet(types.NewPointer(typ.Type())),
				} {
					for i := 0; i < methodSet.Len(); i++ {
						fn := analyzer.ssaProg.MethodValue(methodSet.At(i))
						if fn == nil || fn.Synthetic != "" || visitedMethods[fn] {
							continue
						}
						visitedMethods[fn] = true

						pos := analyzer.fset.Position(fn.Pos())
						if !pos.IsValid() || analyzer.shouldSkipFile(pos.Filename) {
							continue
						}
						totalFuncs++
						reachableCount++
						fileDeclCount[pos.Filename]++
					}
				}
				continue
			}

			for _, methodSet := range []*types.MethodSet{
				analyzer.ssaProg.MethodSets.MethodSet(typ.Type()),
				analyzer.ssaProg.MethodSets.MethodSet(types.NewPointer(typ.Type())),
			} {
				for i := 0; i < methodSet.Len(); i++ {
					fn := analyzer.ssaProg.MethodValue(methodSet.At(i))
					if fn == nil || fn.Synthetic != "" || visitedMethods[fn] {
						continue
					}
					visitedMethods[fn] = true

					pos := analyzer.fset.Position(fn.Pos())
					if !pos.IsValid() {
						continue
					}

					if analyzer.shouldSkipFile(pos.Filename) || hasKeepAnnotation(fn, analyzer.fset) {
						continue
					}

					isDuplicate := false
					for _, d := range dead {
						if d.File == pos.Filename && d.Line == pos.Line && d.Name == fn.Name() {
							isDuplicate = true
							break
						}
					}
					if isDuplicate {
						continue
					}

					totalFuncs++
					fileDeclCount[pos.Filename]++

					if reachable[fn] {
						reachableCount++
						continue
					}

					if analyzer.hasReachableAnon(fn, reachable) {
						reachableCount++
						continue
					}

					isExported := token.IsExported(fn.Name())
					confidence := config.ConfidenceHigh
					if isExported {
						confidence = config.ConfidenceMedium
					}

					if !config.ShouldInclude(confidence, analyzer.cfg.ConfidenceLevel) {
						continue
					}

					receiver := ""
					if fn.Signature.Recv() != nil {
						receiver = fn.Signature.Recv().Type().String()
					}

					dead = append(dead, DeadCode{
						Name:       fn.Name(),
						Kind:       KindFunc,
						File:       pos.Filename,
						Line:       pos.Line,
						Column:     pos.Column,
						Package:    pkg.Pkg.Path(),
						Receiver:   receiver,
						Confidence: confidence,
						IsExported: isExported,
					})
					fileDeadCount[pos.Filename]++
					deadFuncCount++
				}
			}
		}
	}

	// --- Dead vars, consts, types ---

	// Build a cross-package reference count for each package-level object.
	globalRefs := make(map[types.Object]int)
	for _, pkg := range analyzer.allPkgs {
		for _, obj := range pkg.TypesInfo.Defs {
			if obj == nil {
				continue
			}
			if obj.Parent() == obj.Pkg().Scope() {
				if _, exists := globalRefs[obj]; !exists {
					globalRefs[obj] = 0
				}
			}
		}
	}
	for _, pkg := range analyzer.allPkgs {
		for _, obj := range pkg.TypesInfo.Uses {
			if obj == nil {
				continue
			}
			if obj.Parent() == obj.Pkg().Scope() {
				globalRefs[obj]++
			}
		}
	}

	for obj, refCount := range globalRefs {
		var kind DeclKind
		switch o := obj.(type) {
		case *types.Var:
			kind = KindVar
		case *types.Const:
			kind = KindConst
		case *types.TypeName:
			if liveTypes[o] {
				// Type is alive via interface dispatch — count it as a live declaration
				// so the dead-file ratio stays accurate.
				pos := analyzer.fset.Position(obj.Pos())
				if pos.IsValid() && !analyzer.shouldSkipFile(pos.Filename) {
					fileDeclCount[pos.Filename]++
				}
				continue
			}
			kind = KindType
		default:
			continue
		}

		pos := analyzer.fset.Position(obj.Pos())
		if !pos.IsValid() {
			continue
		}

		if analyzer.shouldSkipFile(pos.Filename) {
			continue
		}

		if analyzer.shouldIgnorePackage(obj.Pkg().Path()) {
			continue
		}

		name := obj.Name()
		if name == "_" {
			continue
		}

		// Count every eligible declaration toward the file total.
		fileDeclCount[pos.Filename]++

		if refCount > 0 {
			continue
		}

		isExported := token.IsExported(name)
		confidence := config.ConfidenceHigh
		if isExported {
			confidence = config.ConfidenceMedium
		}

		if !config.ShouldInclude(confidence, analyzer.cfg.ConfidenceLevel) {
			continue
		}

		dead = append(dead, DeadCode{
			Name:       name,
			Kind:       kind,
			File:       pos.Filename,
			Line:       pos.Line,
			Column:     pos.Column,
			Package:    obj.Pkg().Path(),
			Confidence: confidence,
			IsExported: isExported,
		})
		fileDeadCount[pos.Filename]++

		switch kind {
		case KindVar:
			deadVarCount++
		case KindConst:
			deadConstCount++
		case KindType:
			deadTypeCount++
		}
	}

	// Detect fully dead files
	var deadFiles []string
	for file, total := range fileDeclCount {
		if deadCount, exists := fileDeadCount[file]; exists && deadCount >= total && total > 0 {
			deadFiles = append(deadFiles, file)
		}
	}

	return &Result{
		DeadCode: dead,
		Summary: Summary{
			TotalFunctions:     totalFuncs,
			ReachableFunctions: reachableCount,
			DeadFunctions:      deadFuncCount,
			DeadVars:           deadVarCount,
			DeadConsts:         deadConstCount,
			DeadTypes:          deadTypeCount,
			SkippedFiles:       len(skippedFiles),
			DeadFiles:          deadFiles,
		},
	}, nil
}

// hasReachableAnon checks whether any anonymous function defined inside fn
// is in the reachable set — if so, the parent is transitively needed.
func (analyzer *Analyzer) hasReachableAnon(fn *ssa.Function, reachable map[*ssa.Function]bool) bool {
	for _, anon := range fn.AnonFuncs {
		if reachable[anon] {
			return true
		}
	}
	return false
}

// buildLiveTypeSet returns the set of named types that should be considered
// alive even if they have zero direct name references. A type is live if:
//
//  1. Any of its methods (value or pointer receiver) is in the RTA reachable set.
//     This covers port/adapter patterns where the concrete type is only
//     instantiated via a constructor and used exclusively through an interface.
//
//  2. The type implements an interface that has reachable methods. This catches
//     types registered in DI containers where the type name itself never
//     appears in user code.
func (analyzer *Analyzer) buildLiveTypeSet(reachable map[*ssa.Function]bool) map[*types.TypeName]bool {
	liveTypes := make(map[*types.TypeName]bool)

	for _, pkg := range analyzer.ssaPkgs {
		if pkg == nil {
			continue
		}

		for _, member := range pkg.Members {
			ssaType, ok := member.(*ssa.Type)
			if !ok {
				continue
			}

			typeName := ssaType.Object().(*types.TypeName)
			if liveTypes[typeName] {
				continue
			}

			for _, methodSet := range []*types.MethodSet{
				analyzer.ssaProg.MethodSets.MethodSet(ssaType.Type()),
				analyzer.ssaProg.MethodSets.MethodSet(types.NewPointer(ssaType.Type())),
			} {
				for i := 0; i < methodSet.Len(); i++ {
					sel := methodSet.At(i)
					fn := analyzer.ssaProg.MethodValue(sel)
					if fn != nil && reachable[fn] {
						liveTypes[typeName] = true
						break
					}
				}
				if liveTypes[typeName] {
					break
				}
			}
		}
	}

	return liveTypes
}

// shouldIgnorePackage reports whether a package path matches any configured ignore pattern.
func (analyzer *Analyzer) shouldIgnorePackage(pkgPath string) bool {
	for _, pattern := range analyzer.cfg.IgnorePatterns {
		matched, _ := filepath.Match(pattern, pkgPath)
		if matched || strings.Contains(pkgPath, pattern) {
			if analyzer.cfg.Verbose {
				logger.Info("ignoring package: %s (matches %q)", pkgPath, pattern)
			}
			return true
		}
	}
	return false
}

// shouldSkipFile returns true for test files (when configured) and generated files.
func (analyzer *Analyzer) shouldSkipFile(filename string) bool {
	if analyzer.cfg.SkipTests && strings.HasSuffix(filename, "_test.go") {
		return true
	}

	if analyzer.cfg.SkipGenerated && isGeneratedFile(filename) {
		return true
	}

	for _, pattern := range analyzer.cfg.IgnorePatterns {
		matched, _ := filepath.Match(pattern, filepath.Base(filename))
		if matched {
			return true
		}
	}

	return false
}

// hasKeepAnnotation checks if the function's doc comments contain //deadcode:keep.
func hasKeepAnnotation(fn *ssa.Function, fset *token.FileSet) bool {
	syntax := fn.Syntax()
	if syntax == nil {
		return false
	}

	funcDecl, ok := syntax.(*ast.FuncDecl)
	if !ok {
		return false
	}

	if funcDecl.Doc != nil {
		for _, comment := range funcDecl.Doc.List {
			if strings.Contains(comment.Text, "deadcode:keep") {
				return true
			}
		}
	}

	return false
}

// isGeneratedFile reads the first few bytes of a file to check for the
// "Code generated" marker defined by the Go generate convention.
func isGeneratedFile(filename string) bool {
	content, err := os.ReadFile(filename)
	if err != nil {
		return false
	}

	header := string(content)
	if len(header) > 256 {
		header = header[:256]
	}
	return strings.Contains(header, "Code generated") && strings.Contains(header, "DO NOT EDIT")
}
