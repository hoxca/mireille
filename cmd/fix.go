package cmd

import (
	"context"
	"io"
	"log/slog"

	"github.com/hoxca/mireille/internal/fitsheader"
	"github.com/spf13/cobra"
)

func newFixCmd() *cobra.Command {
	opts := &checkOptions{}

	cmd := &cobra.Command{
		Use:   "fix",
		Short: "Rewrite compliant CCD-TEMP values to the expected temperature",
		Long: `Walk --dir recursively and rewrite the CCD-TEMP card of every compliant FITS
file to the exact value given by --temp.

A file is compliant when its CCD-TEMP is within --tolerance of --temp and, when
--gain or --offset is given, when those cards match their flags exactly. Files
that are not compliant are left untouched and listed as rejected. A compliant
file whose CCD-TEMP already equals --temp is counted as unchanged.

Only the 80-byte CCD-TEMP card is overwritten: the keyword, the comment and the
pixel data are preserved, and the file size does not change. Any directory
named rejected, at any depth, is ignored by the scan.

The exit status is non-zero when any file is rejected or cannot be read or
written.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.gainSet = cmd.Flags().Changed("gain")
			opts.offsetSet = cmd.Flags().Changed("offset")
			if err := opts.validate(); err != nil {
				return err
			}
			return runFix(cmd.Context(), cmd.OutOrStdout(), opts)
		},
	}

	addCheckFlags(cmd, opts)
	return cmd
}

func runFix(ctx context.Context, out io.Writer, opts *checkOptions) error {
	paths, err := collectFITS(opts.dir)
	if err != nil {
		return err
	}
	slog.Debug("fixing directory", "dir", opts.dir, "files", len(paths))

	var fixed, unchanged int
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
		if len(problems) > 0 {
			rejected = append(rejected, checkResult{path: path, problems: problems})
			continue
		}

		temp, found, err := fitsheader.ReadFloat(path, tempKeyword)
		if err != nil {
			slog.Debug("unable to read header", "path", path, "err", err)
			errs = append(errs, fileError{path: path, err: err})
			continue
		}
		if !found {
			rejected = append(rejected, checkResult{
				path:     path,
				problems: []string{tempKeyword + " missing"},
			})
			continue
		}
		if temp == opts.temp {
			unchanged++
			slog.Debug("already correct", "path", path, "temp", temp)
			continue
		}

		if _, err := fitsheader.WriteFloat(path, tempKeyword, opts.temp); err != nil {
			slog.Debug("unable to rewrite header", "path", path, "err", err)
			errs = append(errs, fileError{path: path, err: err})
			continue
		}
		fixed++
		slog.Debug("rewrote CCD-TEMP", "path", path, "from", temp, "to", opts.temp)
	}

	if err := printFixReport(out, fixed, unchanged, rejected, errs); err != nil {
		return err
	}

	if len(rejected) > 0 || len(errs) > 0 {
		return errRejected
	}
	return nil
}

func printFixReport(out io.Writer, fixed, unchanged int, rejected []checkResult, errs []fileError) error {
	rw := &reportWriter{w: out}
	rw.printf("fixed:     %d\n", fixed)
	rw.printf("unchanged: %d\n", unchanged)
	rw.printf("rejected:  %d\n", len(rejected))

	printRejected(rw, rejected)
	printErrors(rw, errs)

	return rw.err
}
