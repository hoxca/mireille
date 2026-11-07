package cmd

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/hoxca/mireille/internal/fitsheader"
	"github.com/spf13/cobra"
)

// tempKeyword is the FITS card that records the sensor temperature.
const tempKeyword = "CCD-TEMP"

// epsilon absorbs rounding in the header so that a -15.0 card matches a -15
// target at zero tolerance.
const epsilon = 1e-3

type checkOptions struct {
	dir       string
	temp      float64
	tolerance float64
}

func newCheckCmd() *cobra.Command {
	opts := &checkOptions{}

	cmd := &cobra.Command{
		Use:   "check",
		Short: "Verify the CCD-TEMP of every FITS file under a directory",
		Long: `Walk --dir recursively and compare the CCD-TEMP of every FITS file with
--temp. Files whose recorded temperature differs by more than --tolerance are
rejected. A file without a CCD-TEMP card is rejected and marked as missing.

Only .fits, .fit and .fts files are considered; every other file and every
hidden file is ignored. The report counts the compliant and rejected files and
lists the rejected ones. The exit status is non-zero when any file is rejected
or unreadable.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.validate(); err != nil {
				return err
			}
			return runCheck(cmd.Context(), cmd.OutOrStdout(), opts)
		},
	}

	flags := cmd.Flags()
	flags.StringVarP(&opts.dir, "dir", "d", "",
		"directory to scan recursively for FITS files")
	flags.Float64VarP(&opts.temp, "temp", "t", 0,
		"compliant sensor temperature in degrees Celsius")
	flags.Float64Var(&opts.tolerance, "tolerance", 0,
		"allowed absolute deviation from --temp in degrees Celsius")
	_ = cmd.MarkFlagRequired("dir")
	_ = cmd.MarkFlagRequired("temp")
	return cmd
}

func (o *checkOptions) validate() error {
	if o.dir == "" {
		return fmt.Errorf("--dir must not be empty")
	}
	info, err := os.Stat(o.dir)
	if err != nil {
		return fmt.Errorf("--dir: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("--dir %s is not a directory", o.dir)
	}
	if math.IsNaN(o.temp) || math.IsInf(o.temp, 0) {
		return fmt.Errorf("--temp must be a finite number, got %v", o.temp)
	}
	if math.IsNaN(o.tolerance) || o.tolerance < 0 {
		return fmt.Errorf("--tolerance must not be negative, got %v", o.tolerance)
	}
	return nil
}

// checkResult is one rejected file.
type checkResult struct {
	path   string
	temp   float64
	reason string
}

// fileError is a FITS file that could not be read.
type fileError struct {
	path string
	err  error
}

func runCheck(ctx context.Context, out io.Writer, opts *checkOptions) error {
	paths, err := collectFITS(opts.dir)
	if err != nil {
		return err
	}
	slog.Debug("scanning directory", "dir", opts.dir, "files", len(paths))

	var compliant int
	var rejected []checkResult
	var errs []fileError

	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}

		temp, found, err := fitsheader.ReadFloat(path, tempKeyword)
		if err != nil {
			slog.Debug("unable to read header", "path", path, "err", err)
			errs = append(errs, fileError{path: path, err: err})
			continue
		}
		if !found {
			rejected = append(rejected, checkResult{path: path, reason: tempKeyword + " missing"})
			continue
		}
		if math.Abs(temp-opts.temp) <= opts.tolerance+epsilon {
			compliant++
			slog.Debug("compliant", "path", path, "temp", temp)
			continue
		}
		rejected = append(rejected, checkResult{path: path, temp: temp})
	}

	if err := printReport(out, opts, compliant, rejected, errs); err != nil {
		return err
	}

	if len(rejected) > 0 || len(errs) > 0 {
		return errRejected
	}
	return nil
}

// reportWriter accumulates the first write error so the report can be built
// with plain printf-style calls without ignoring their return values.
type reportWriter struct {
	w   io.Writer
	err error
}

func (rw *reportWriter) printf(format string, args ...any) {
	if rw.err == nil {
		_, rw.err = fmt.Fprintf(rw.w, format, args...)
	}
}

func (rw *reportWriter) println(args ...any) {
	if rw.err == nil {
		_, rw.err = fmt.Fprintln(rw.w, args...)
	}
}

func printReport(out io.Writer, opts *checkOptions, compliant int, rejected []checkResult, errs []fileError) error {
	rw := &reportWriter{w: out}
	rw.printf("compliant: %d\n", compliant)
	rw.printf("rejected:  %d\n", len(rejected))

	if len(rejected) > 0 {
		rw.println("\nrejected:")
		for _, r := range rejected {
			if r.reason != "" {
				rw.printf("  %s  %s\n", r.path, r.reason)
				continue
			}
			rw.printf("  %s  %s=%s (want %s", r.path, tempKeyword,
				formatTemp(r.temp), formatTemp(opts.temp))
			if opts.tolerance > 0 {
				rw.printf(", tol %s", formatTemp(opts.tolerance))
			}
			rw.println(")")
		}
	}

	if len(errs) > 0 {
		rw.println("\nerrors:")
		for _, e := range errs {
			rw.printf("  %s  %v\n", e.path, e.err)
		}
	}

	return rw.err
}

func formatTemp(v float64) string {
	return fmt.Sprintf("%.2f", v)
}

// collectFITS resolves root into a sorted list of FITS files. Directories are
// walked recursively; hidden files and non-FITS extensions are skipped.
func collectFITS(root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !isFITS(path) {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.Sort(paths)
	return paths, nil
}

func isFITS(path string) bool {
	base := filepath.Base(path)
	if strings.HasPrefix(base, ".") {
		// Skip ._foo.fits resource forks and other hidden files.
		return false
	}
	switch strings.ToLower(filepath.Ext(base)) {
	case ".fits", ".fit", ".fts":
		return true
	default:
		return false
	}
}
