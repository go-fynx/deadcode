package main

import (
	"fmt"
	"os"

	"github.com/go-fynx/deadcode/internal/analyzer"
	"github.com/go-fynx/deadcode/internal/color"
	"github.com/go-fynx/deadcode/internal/config"
	"github.com/go-fynx/deadcode/internal/fixer"
	"github.com/go-fynx/deadcode/internal/logger"
	"github.com/go-fynx/deadcode/internal/report"
)

func main() {
	if err := run(); err != nil {
		logger.Error("deadcode:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Parse()
	if err != nil {
		return err
	}

	logger.Header("deadcode", "— dead code detector for Go")

	a := analyzer.New(cfg)
	result, err := a.Run()
	if err != nil {
		return err
	}

	if err := report.Write(os.Stdout, result, cfg); err != nil {
		return fmt.Errorf("writing report: %w", err)
	}

	if !cfg.Fix && !cfg.FixDryRun {
		return nil
	}

	return applyFix(cfg, result)
}

func applyFix(cfg *config.Config, result *analyzer.Result) error {
	f := fixer.New(cfg)

	if cfg.FixDryRun {
		logger.Banner("Dry Run (no files modified)", color.BoldYellow)
	} else {
		logger.Banner("Applying fixes", color.BoldRed)
	}

	fixResult, err := f.Fix(result)
	if err != nil {
		return fmt.Errorf("applying fixes: %w", err)
	}

	fixer.PrintFixResult(fixResult)
	return nil
}
