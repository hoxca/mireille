// Package cmd implements the mireille command line.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

// Version and BuildDate are injected by the Makefile through -ldflags.
var (
	Version   = "v0"
	BuildDate = "unknown"
)

// errRejected reports that the audit found non-compliant or unreadable files.
// The report has already been printed, so Execute returns it without a message.
var errRejected = errors.New("one or more files were rejected")

var verbose bool

// Execute runs the command line and reports whether it succeeded.
func Execute() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err := newRootCmd().ExecuteContext(ctx)
	if err != nil {
		if errors.Is(err, errRejected) {
			return err
		}
		fmt.Fprintf(os.Stderr, "mireille: %v\n", err)
		return err
	}
	return nil
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "mireille",
		Short: "Verify dark frame temperature consistency",
		Long: `mireille audits the sensor temperature recorded in the FITS headers of dark
frames.

check walks a directory recursively, reads the CCD-TEMP card of every FITS file
and compares it with the temperature given on the command line. Files within
--tolerance of --temp are compliant; the others are listed in the report. The
exit status is non-zero when any file is rejected.`,
		Version:       fmt.Sprintf("%s (built %s)", Version, BuildDate),
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRun: func(*cobra.Command, []string) {
			setupLogging()
		},
	}
	root.SetVersionTemplate("mireille {{.Version}}\n")
	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "log debugging detail")

	root.AddCommand(newCheckCmd())
	root.AddCommand(newFixCmd())
	root.AddCommand(newFixTypeCmd())
	return root
}

func setupLogging() {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(handler))
}
