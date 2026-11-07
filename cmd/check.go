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
	"strconv"
	"strings"

	"github.com/hoxca/mireille/internal/fitsheader"
	"github.com/spf13/cobra"
)

// tempKeyword is the FITS card that records the sensor temperature.
const tempKeyword = "CCD-TEMP"

// gainKeyword is the FITS card that records the sensor gain.
const gainKeyword = "GAIN"

// offsetKeyword is the FITS card that records the sensor offset.
const offsetKeyword = "OFFSET"

// epsilon absorbs rounding in the header so that a -15.0 card matches a -15
// target at zero tolerance.
const epsilon = 1e-3

type checkOptions struct {
	dir       string
	temp      float64
	tolerance float64
	gain      float64
	// gainSet reports whether --gain was supplied. Gain is checked only then.
	gainSet bool
	offset  float64
	// offsetSet reports whether --offset was supplied. Offset is checked only
	// then.
	offsetSet bool
}

func newCheckCmd() *cobra.Command {
	opts := &checkOptions{}

	cmd := &cobra.Command{
		Use:   "check",
		Short: "Verify the CCD-TEMP of every FITS file under a directory",
		Long: `Walk --dir recursively and compare the CCD-TEMP of every FITS file with
--temp. Files whose recorded temperature differs by more than --tolerance are
rejected. A file without a CCD-TEMP card is rejected and marked as missing.

When --gain is given, the GAIN card is checked too and must equal the flag
exactly; there is no tolerance for the gain. A file without a GAIN card is
rejected and marked as missing.

When --offset is given, the OFFSET card is checked the same way: it must equal
the flag exactly, and a file without an OFFSET card is rejected and marked as
missing.

Only .fits, .fit and .fts files are considered; every other file and every
hidden file is ignored. The report counts the compliant and rejected files and
lists the rejected ones. The exit status is non-zero when any file is rejected
or unreadable.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.gainSet = cmd.Flags().Changed("gain")
			opts.offsetSet = cmd.Flags().Changed("offset")
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
	flags.Float64Var(&opts.gain, "gain", 0,
		"compliant gain from the GAIN card; checked only when given")
	flags.Float64Var(&opts.offset, "offset", 0,
		"compliant offset from the OFFSET card; checked only when given")
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
	if o.gainSet && (math.IsNaN(o.gain) || math.IsInf(o.gain, 0)) {
		return fmt.Errorf("--gain must be a finite number, got %v", o.gain)
	}
	if o.offsetSet && (math.IsNaN(o.offset) || math.IsInf(o.offset, 0)) {
		return fmt.Errorf("--offset must be a finite number, got %v", o.offset)
	}
	return nil
}

// checkResult is one rejected file, with one entry per failed check.
type checkResult struct {
	path     string
	problems []string
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

		problems, err := opts.fileProblems(path)
		if err != nil {
			slog.Debug("unable to read header", "path", path, "err", err)
			errs = append(errs, fileError{path: path, err: err})
			continue
		}
		if len(problems) == 0 {
			compliant++
			slog.Debug("compliant", "path", path)
			continue
		}
		rejected = append(rejected, checkResult{path: path, problems: problems})
	}

	if err := printReport(out, opts, compliant, rejected, errs); err != nil {
		return err
	}

	if len(rejected) > 0 || len(errs) > 0 {
		return errRejected
	}
	return nil
}

// fileProblems reports every enabled check the file fails. A read error is
// returned as-is so the caller can list the file separately.
func (o *checkOptions) fileProblems(path string) ([]string, error) {
	var problems []string

	temp, found, err := fitsheader.ReadFloat(path, tempKeyword)
	if err != nil {
		return nil, err
	}
	if !found {
		problems = append(problems, tempKeyword+" missing")
	} else if math.Abs(temp-o.temp) > o.tolerance+epsilon {
		msg := fmt.Sprintf("%s=%s (want %s", tempKeyword, formatTemp(temp), formatTemp(o.temp))
		if o.tolerance > 0 {
			msg += fmt.Sprintf(", tol %s", formatTemp(o.tolerance))
		}
		problems = append(problems, msg+")")
	}

	if o.gainSet {
		gain, found, err := fitsheader.ReadFloat(path, gainKeyword)
		if err != nil {
			return nil, err
		}
		if !found {
			problems = append(problems, gainKeyword+" missing")
		} else if gain != o.gain {
			problems = append(problems,
				fmt.Sprintf("%s=%s (want %s)", gainKeyword, formatFixed(gain), formatFixed(o.gain)))
		}
	}

	if !o.offsetSet {
		return problems, nil
	}

	offset, found, err := fitsheader.ReadFloat(path, offsetKeyword)
	if err != nil {
		return nil, err
	}
	if !found {
		problems = append(problems, offsetKeyword+" missing")
	} else if offset != o.offset {
		problems = append(problems,
			fmt.Sprintf("%s=%s (want %s)", offsetKeyword, formatFixed(offset), formatFixed(o.offset)))
	}

	return problems, nil
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
			rw.printf("  %s  %s\n", r.path, strings.Join(r.problems, "; "))
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

func formatFixed(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
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
