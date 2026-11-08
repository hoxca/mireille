package cmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/hoxca/mireille/internal/fitsheader"
	"github.com/spf13/cobra"
)

// typeKeyword is the FITS card that records the image type.
const typeKeyword = "IMAGETYP"

// Image types written by fixtype.
const (
	darkFrame  = "Dark Frame"
	flatField  = "Flat Field"
	biasFrame  = "Bias Frame"
	lightFrame = "Light Frame"
)

func newFixTypeCmd() *cobra.Command {
	var (
		dir   string
		dark  bool
		flat  bool
		bias  bool
		light bool
	)

	cmd := &cobra.Command{
		Use:   "fixtype",
		Short: "Rewrite the IMAGETYP card of every FITS file under a directory",
		Long: `Walk --dir recursively and rewrite the IMAGETYP card of every FITS file to
the image type selected by --dark, --flat, --bias or --light.

--dark writes 'Dark Frame', --flat writes 'Flat Field', --bias writes
'Bias Frame' and --light writes 'Light Frame'. Exactly one of the four must be
given. A file whose IMAGETYP already holds the requested value is counted as
compliant. A file without an IMAGETYP card is reported as an error and left
untouched.

Only the 80-byte IMAGETYP card is overwritten: the keyword, the comment and the
pixel data are preserved, and the file size does not change. Any directory
named rejected, at any depth, is ignored by the scan.

The exit status is non-zero when any file cannot be read or written, or has no
IMAGETYP card.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateDir(dir); err != nil {
				return err
			}
			value := darkFrame
			switch {
			case flat:
				value = flatField
			case bias:
				value = biasFrame
			case light:
				value = lightFrame
			}
			return runFixType(cmd.Context(), cmd.OutOrStdout(), dir, value)
		},
	}

	flags := cmd.Flags()
	flags.StringVarP(&dir, "dir", "d", "",
		"directory to scan recursively for FITS files")
	flags.BoolVar(&dark, "dark", false, "rewrite IMAGETYP to 'Dark Frame'")
	flags.BoolVar(&flat, "flat", false, "rewrite IMAGETYP to 'Flat Field'")
	flags.BoolVar(&bias, "bias", false, "rewrite IMAGETYP to 'Bias Frame'")
	flags.BoolVar(&light, "light", false, "rewrite IMAGETYP to 'Light Frame'")
	_ = cmd.MarkFlagRequired("dir")
	cmd.MarkFlagsOneRequired("dark", "flat", "bias", "light")
	cmd.MarkFlagsMutuallyExclusive("dark", "flat", "bias", "light")
	return cmd
}

// typeResult is one file whose IMAGETYP was rewritten.
type typeResult struct {
	path string
	from string
	to   string
}

func runFixType(ctx context.Context, out io.Writer, dir, value string) error {
	paths, err := collectFITS(dir)
	if err != nil {
		return err
	}
	slog.Debug("fixing image types", "dir", dir, "files", len(paths), "type", value)

	var fixed, compliant int
	var rewrites []typeResult
	var errs []fileError

	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}

		current, found, err := fitsheader.ReadString(path, typeKeyword)
		if err != nil {
			slog.Debug("unable to read header", "path", path, "err", err)
			errs = append(errs, fileError{path: path, err: err})
			continue
		}
		if !found {
			errs = append(errs, fileError{path: path, err: fmt.Errorf("%s missing", typeKeyword)})
			continue
		}
		if current == value {
			compliant++
			slog.Debug("already compliant", "path", path, "type", current)
			continue
		}

		if _, err := fitsheader.WriteString(path, typeKeyword, value); err != nil {
			slog.Debug("unable to rewrite header", "path", path, "err", err)
			errs = append(errs, fileError{path: path, err: err})
			continue
		}
		fixed++
		rewrites = append(rewrites, typeResult{path: path, from: current, to: value})
		slog.Debug("rewrote IMAGETYP", "path", path, "from", current, "to", value)
	}

	if err := printFixTypeReport(out, fixed, compliant, rewrites, errs); err != nil {
		return err
	}

	if len(errs) > 0 {
		return errRejected
	}
	return nil
}

func printFixTypeReport(out io.Writer, fixed, compliant int, rewrites []typeResult, errs []fileError) error {
	rw := &reportWriter{w: out}
	rw.printf("fixed:     %d\n", fixed)
	rw.printf("compliant: %d\n", compliant)

	if len(rewrites) > 0 {
		rw.println("\nRewriting")
		for _, r := range rewrites {
			rw.printf("  %s  %s='%s' (rewrite '%s')\n", r.path, typeKeyword, r.from, r.to)
		}
	}

	printErrors(rw, errs)

	return rw.err
}
