package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hoxca/mireille/internal/fitsheader"
)

func TestFixTypeDark(t *testing.T) {
	dir := t.TempDir()
	changed := writeFITS(t, dir, "a.fits", "IMAGETYP= 'Bias Frame'")
	compliant := writeFITS(t, dir, "b.fits", "IMAGETYP= 'Dark Frame'")
	missing := writeFITS(t, dir, "c.fits", "EXPTIME =                 600.0")

	var out bytes.Buffer
	err := runFixType(context.Background(), &out, dir, darkFrame)
	if !errors.Is(err, errRejected) {
		t.Fatalf("runFixType() = %v, want errRejected\n%s", err, out.String())
	}

	got, ok, err := fitsheader.ReadString(changed, typeKeyword)
	if err != nil || !ok || got != darkFrame {
		t.Errorf("%s: IMAGETYP = %q, %v, %v, want %q", changed, got, ok, err, darkFrame)
	}
	got, _, err = fitsheader.ReadString(compliant, typeKeyword)
	if err != nil || got != darkFrame {
		t.Errorf("%s: IMAGETYP = %q, %v, want unchanged", compliant, got, err)
	}
	if got, _, err := fitsheader.ReadString(missing, typeKeyword); err != nil || got != "" {
		t.Errorf("%s: missing file should be untouched", missing)
	}

	report := out.String()
	for _, want := range []string{
		"fixed:     1\n",
		"compliant: 1\n",
		"\nRewriting\n",
		changed + "  IMAGETYP='Bias Frame' (rewrite 'Dark Frame')\n",
		missing + "  IMAGETYP missing\n",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("report missing %q:\n%s", want, report)
		}
	}
}

func TestFixTypeFlat(t *testing.T) {
	dir := t.TempDir()
	path := writeFITS(t, dir, "a.fits", "IMAGETYP= 'Light Frame'")

	var out bytes.Buffer
	if err := runFixType(context.Background(), &out, dir, flatField); err != nil {
		t.Fatalf("runFixType(): %v\n%s", err, out.String())
	}
	got, ok, err := fitsheader.ReadString(path, typeKeyword)
	if err != nil || !ok || got != flatField {
		t.Fatalf("IMAGETYP = %q, %v, %v, want %q", got, ok, err, flatField)
	}
}

func TestFixTypeBias(t *testing.T) {
	dir := t.TempDir()
	changed := writeFITS(t, dir, "a.fits", "IMAGETYP= 'Light Frame'")
	compliant := writeFITS(t, dir, "b.fits", "IMAGETYP= 'Bias Frame'")

	var out bytes.Buffer
	if err := runFixType(context.Background(), &out, dir, biasFrame); err != nil {
		t.Fatalf("runFixType(): %v\n%s", err, out.String())
	}
	got, ok, err := fitsheader.ReadString(changed, typeKeyword)
	if err != nil || !ok || got != biasFrame {
		t.Errorf("%s: IMAGETYP = %q, %v, %v, want %q", changed, got, ok, err, biasFrame)
	}
	got, _, err = fitsheader.ReadString(compliant, typeKeyword)
	if err != nil || got != biasFrame {
		t.Errorf("%s: IMAGETYP = %q, %v, want unchanged", compliant, got, err)
	}
	if !strings.Contains(out.String(), "fixed:     1\n") ||
		!strings.Contains(out.String(), "compliant: 1\n") {
		t.Errorf("unexpected report:\n%s", out.String())
	}
}

func TestFixTypeIgnoresRejectedDirs(t *testing.T) {
	dir := t.TempDir()
	path := writeFITS(t, dir, "rejected/a.fits", "IMAGETYP= 'Bias Frame'")

	var out bytes.Buffer
	if err := runFixType(context.Background(), &out, dir, darkFrame); err != nil {
		t.Fatalf("runFixType(): %v\n%s", err, out.String())
	}
	got, _, err := fitsheader.ReadString(path, typeKeyword)
	if err != nil {
		t.Fatal(err)
	}
	if got != "Bias Frame" {
		t.Errorf("file under rejected/ was rewritten to %q", got)
	}
}

func TestFixTypeCommand(t *testing.T) {
	dir := t.TempDir()
	path := writeFITS(t, dir, "a.fits", "IMAGETYP= 'Bias Frame'")

	cmd := newFixTypeCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--dir", dir, "--dark"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v\n%s", err, out.String())
	}
	got, ok, err := fitsheader.ReadString(path, typeKeyword)
	if err != nil || !ok || got != darkFrame {
		t.Fatalf("IMAGETYP = %q, %v, %v, want %q", got, ok, err, darkFrame)
	}
	if _, err := os.Stat(filepath.Join(dir, rejectedDirName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fixtype created a rejected directory")
	}
}

func TestFixTypeCommandFlags(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{name: "dark", args: []string{"--dir", dir, "--dark"}},
		{name: "flat", args: []string{"--dir", dir, "--flat"}},
		{name: "bias", args: []string{"--dir", dir, "--bias"}},
		{name: "neither", args: []string{"--dir", dir}, wantErr: true},
		{name: "both", args: []string{"--dir", dir, "--dark", "--flat"}, wantErr: true},
		{name: "dark and bias", args: []string{"--dir", dir, "--dark", "--bias"}, wantErr: true},
		{name: "flat and bias", args: []string{"--dir", dir, "--flat", "--bias"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newFixTypeCmd()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs(tt.args)

			err := cmd.Execute()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Execute() = %v, want error=%v\n%s", err, tt.wantErr, out.String())
			}
		})
	}
}
