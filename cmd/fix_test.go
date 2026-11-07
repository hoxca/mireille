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

func TestFix(t *testing.T) {
	dir := t.TempDir()
	first := writeFITS(t, dir, "a.fits", "CCD-TEMP=                 0.05")
	second := writeFITS(t, dir, "nested/b.fits", "CCD-TEMP=                -0.08")
	exact := writeFITS(t, dir, "c.fits", "CCD-TEMP=                   0")
	rejected := writeFITS(t, dir, "d.fits", "CCD-TEMP=                 0.5")

	var out bytes.Buffer
	err := runFix(context.Background(), &out, &checkOptions{dir: dir, temp: 0, tolerance: 0.1})
	if !errors.Is(err, errRejected) {
		t.Fatalf("runFix() = %v, want errRejected\n%s", err, out.String())
	}

	for _, path := range []string{first, second} {
		got, ok, err := fitsheader.ReadFloat(path, "CCD-TEMP")
		if err != nil || !ok || got != 0 {
			t.Errorf("%s: CCD-TEMP = %v, %v, %v, want 0", path, got, ok, err)
		}
	}
	for _, path := range []string{exact, rejected} {
		got, ok, err := fitsheader.ReadFloat(path, "CCD-TEMP")
		if err != nil || !ok {
			t.Errorf("%s: CCD-TEMP read failed: %v", path, err)
			continue
		}
		if path == exact && got != 0 {
			t.Errorf("%s: CCD-TEMP = %v, want unchanged 0", path, got)
		}
		if path == rejected && got != 0.5 {
			t.Errorf("%s: rejected file was rewritten to %v", path, got)
		}
	}

	report := out.String()
	for _, want := range []string{
		"fixed:     2\n",
		"unchanged: 1\n",
		"rejected:  1\n",
		rejected,
	} {
		if !strings.Contains(report, want) {
			t.Errorf("report missing %q:\n%s", want, report)
		}
	}
}

func TestFixGainGate(t *testing.T) {
	tests := []struct {
		name    string
		cards   []string
		gain    int
		gainSet bool
		want    float64
		changed bool
	}{
		{
			name:    "matching gain is fixed",
			cards:   []string{"CCD-TEMP=                 0.05", "GAIN    =                  100"},
			gain:    100,
			gainSet: true,
			changed: true,
		},
		{
			name:    "mismatched gain is untouched",
			cards:   []string{"CCD-TEMP=                 0.05", "GAIN    =                  200"},
			gain:    100,
			gainSet: true,
			want:    0.05,
		},
		{
			name:    "missing gain is untouched",
			cards:   []string{"CCD-TEMP=                 0.05"},
			gain:    100,
			gainSet: true,
			want:    0.05,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := writeFITS(t, dir, "a.fits", tt.cards...)

			var out bytes.Buffer
			err := runFix(context.Background(), &out, &checkOptions{
				dir: dir, temp: 0, tolerance: 0.1, gain: tt.gain, gainSet: tt.gainSet,
			})
			wantErr := tt.gainSet && tt.want != 0
			if gotErr := errors.Is(err, errRejected); gotErr != wantErr {
				t.Fatalf("err = %v, want rejected=%v\n%s", err, wantErr, out.String())
			}

			got, ok, err := fitsheader.ReadFloat(path, "CCD-TEMP")
			if err != nil || !ok {
				t.Fatalf("CCD-TEMP read failed: %v", err)
			}
			if tt.changed && got != 0 {
				t.Errorf("CCD-TEMP = %v, want rewritten to 0", got)
			}
			if !tt.changed && got != tt.want {
				t.Errorf("CCD-TEMP = %v, want untouched %v", got, tt.want)
			}
		})
	}
}

func TestFixIgnoresRejectedDirs(t *testing.T) {
	dir := t.TempDir()
	path := writeFITS(t, dir, "rejected/a.fits", "CCD-TEMP=                 0.05")

	var out bytes.Buffer
	if err := runFix(context.Background(), &out, &checkOptions{dir: dir, temp: 0, tolerance: 0.1}); err != nil {
		t.Fatalf("runFix(): %v\n%s", err, out.String())
	}
	got, _, err := fitsheader.ReadFloat(path, "CCD-TEMP")
	if err != nil {
		t.Fatal(err)
	}
	if got != 0.05 {
		t.Errorf("file under rejected/ was rewritten to %v", got)
	}
}

func TestFixCommand(t *testing.T) {
	dir := t.TempDir()
	path := writeFITS(t, dir, "a.fits", "CCD-TEMP=                 0.05")

	cmd := newFixCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--dir", dir, "--temp", "0", "--tolerance", "0.1"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v\n%s", err, out.String())
	}
	got, ok, err := fitsheader.ReadFloat(path, "CCD-TEMP")
	if err != nil || !ok || got != 0 {
		t.Fatalf("CCD-TEMP = %v, %v, %v, want 0", got, ok, err)
	}
	if _, err := os.Stat(filepath.Join(dir, rejectedDirName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fix created a rejected directory")
	}
}
