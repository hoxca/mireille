package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFITS writes a minimal primary header with the given cards into dir.
func writeFITS(t *testing.T, dir, name string, cards ...string) string {
	t.Helper()

	var b strings.Builder
	line := func(s string) {
		if len(s) > 80 {
			t.Fatalf("card %q is longer than 80 bytes", s)
		}
		b.WriteString(s)
		b.WriteString(strings.Repeat(" ", 80-len(s)))
	}

	line("SIMPLE  =                    T")
	for _, card := range cards {
		line(card)
	}
	line("END")
	for b.Len()%2880 != 0 {
		b.WriteByte(' ')
	}

	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write FITS: %v", err)
	}
	return path
}

func TestCheckReport(t *testing.T) {
	dir := t.TempDir()
	good := writeFITS(t, dir, "a.fits", "CCD-TEMP=                -15.0")
	writeFITS(t, dir, "nested/b.FIT", "CCD-TEMP=                -15.0")
	bad := writeFITS(t, dir, "c.fit", "CCD-TEMP=                -10.0")
	missing := writeFITS(t, dir, "d.fts", "EXPTIME =                600.0")

	// Ignored: not FITS, or hidden.
	writeFITS(t, dir, "notes.txt", "CCD-TEMP=                -10.0")
	writeFITS(t, dir, ".hidden.fits", "CCD-TEMP=                -10.0")

	var out bytes.Buffer
	err := runCheck(context.Background(), &out, &checkOptions{dir: dir, temp: -15})
	if !errors.Is(err, errRejected) {
		t.Fatalf("runCheck() = %v, want errRejected", err)
	}

	report := out.String()
	for _, want := range []string{
		"compliant: 2\n",
		"rejected:  2\n",
		bad,
		missing,
		"CCD-TEMP missing",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("report missing %q:\n%s", want, report)
		}
	}
	for _, unwanted := range []string{good, "notes.txt", ".hidden.fits"} {
		if strings.Contains(report, unwanted) {
			t.Errorf("report unexpectedly contains %q:\n%s", unwanted, report)
		}
	}
}

func TestCheckAllCompliant(t *testing.T) {
	dir := t.TempDir()
	writeFITS(t, dir, "a.fits", "CCD-TEMP=                -15.0")
	writeFITS(t, dir, "b.fits", "CCD-TEMP=             -15.0005")

	var out bytes.Buffer
	if err := runCheck(context.Background(), &out, &checkOptions{dir: dir, temp: -15}); err != nil {
		t.Fatalf("runCheck(): %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "compliant: 2\n") {
		t.Fatalf("unexpected report:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "rejected:  0\n") {
		t.Fatalf("unexpected report:\n%s", out.String())
	}
}

func TestCheckEpsilon(t *testing.T) {
	tests := []struct {
		name    string
		card    string
		wantErr bool
	}{
		{name: "inside epsilon", card: "CCD-TEMP=             -15.0005"},
		{name: "outside epsilon", card: "CCD-TEMP=             -15.002", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFITS(t, dir, "a.fits", tt.card)

			var out bytes.Buffer
			err := runCheck(context.Background(), &out, &checkOptions{dir: dir, temp: -15})
			if gotErr := errors.Is(err, errRejected); gotErr != tt.wantErr {
				t.Fatalf("err = %v, want rejected=%v\n%s", err, tt.wantErr, out.String())
			}
		})
	}
}

func TestCheckTolerance(t *testing.T) {
	dir := t.TempDir()
	writeFITS(t, dir, "a.fits", "CCD-TEMP=                -15.4")

	tests := []struct {
		tolerance float64
		wantErr   bool
	}{
		{tolerance: 0.5, wantErr: false},
		{tolerance: 0.1, wantErr: true},
	}
	for _, tt := range tests {
		var out bytes.Buffer
		err := runCheck(context.Background(), &out, &checkOptions{
			dir: dir, temp: -15, tolerance: tt.tolerance,
		})
		if gotErr := errors.Is(err, errRejected); gotErr != tt.wantErr {
			t.Errorf("tolerance %v: err = %v, want rejected=%v\n%s", tt.tolerance, err, tt.wantErr, out.String())
		}
	}
}

func TestCheckUnreadable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.fits")
	if err := os.WriteFile(path, []byte("not a fits header"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	err := runCheck(context.Background(), &out, &checkOptions{dir: dir, temp: -15})
	if !errors.Is(err, errRejected) {
		t.Fatalf("runCheck() = %v, want errRejected", err)
	}
	if !strings.Contains(out.String(), "errors:") || !strings.Contains(out.String(), path) {
		t.Fatalf("unexpected report:\n%s", out.String())
	}
}

func TestCheckGain(t *testing.T) {
	tests := []struct {
		name         string
		cards        []string
		gain         float64
		gainSet      bool
		wantRejected bool
		want         []string
	}{
		{
			name:    "matching gain",
			cards:   []string{"CCD-TEMP=                -15.0", "GAIN    =                  100"},
			gain:    100,
			gainSet: true,
		},
		{
			name:         "mismatched gain",
			cards:        []string{"CCD-TEMP=                -15.0", "GAIN    =                  200"},
			gain:         100,
			gainSet:      true,
			wantRejected: true,
			want:         []string{"GAIN=200 (want 100)"},
		},
		{
			name:         "missing gain",
			cards:        []string{"CCD-TEMP=                -15.0"},
			gain:         100,
			gainSet:      true,
			wantRejected: true,
			want:         []string{"GAIN missing"},
		},
		{
			name:    "gain ignored without flag",
			cards:   []string{"CCD-TEMP=                -15.0", "GAIN    =                  200"},
			gain:    100,
			gainSet: false,
		},
		{
			name:         "temperature and gain both fail",
			cards:        []string{"CCD-TEMP=                -10.0", "GAIN    =                  200"},
			gain:         100,
			gainSet:      true,
			wantRejected: true,
			want:         []string{"CCD-TEMP=-10.00 (want -15.00)", "GAIN=200 (want 100)"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFITS(t, dir, "a.fits", tt.cards...)

			var out bytes.Buffer
			err := runCheck(context.Background(), &out, &checkOptions{
				dir: dir, temp: -15, gain: tt.gain, gainSet: tt.gainSet,
			})
			if gotErr := errors.Is(err, errRejected); gotErr != tt.wantRejected {
				t.Fatalf("err = %v, want rejected=%v\n%s", err, tt.wantRejected, out.String())
			}
			for _, want := range tt.want {
				if !strings.Contains(out.String(), want) {
					t.Errorf("report missing %q:\n%s", want, out.String())
				}
			}
		})
	}
}

func TestCheckGainFlag(t *testing.T) {
	dir := t.TempDir()
	writeFITS(t, dir, "a.fits",
		"CCD-TEMP=                -15.0",
		"GAIN    =                  100")

	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{
			name: "gain given and matching",
			args: []string{"--dir", dir, "--temp", "-15", "--gain", "100"},
		},
		{
			name:    "gain given and mismatched",
			args:    []string{"--dir", dir, "--temp", "-15", "--gain", "200"},
			wantErr: true,
		},
		{
			name: "gain omitted",
			args: []string{"--dir", dir, "--temp", "-15"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newCheckCmd()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetArgs(tt.args)

			err := cmd.Execute()
			if gotErr := errors.Is(err, errRejected); gotErr != tt.wantErr {
				t.Fatalf("Execute() = %v, want rejected=%v\n%s", err, tt.wantErr, out.String())
			}
		})
	}
}

func TestCheckOffset(t *testing.T) {
	tests := []struct {
		name         string
		cards        []string
		gain         float64
		gainSet      bool
		offset       float64
		offsetSet    bool
		wantRejected bool
		want         []string
	}{
		{
			name:      "matching offset",
			cards:     []string{"CCD-TEMP=                -15.0", "OFFSET  =                   30"},
			offset:    30,
			offsetSet: true,
		},
		{
			name:         "mismatched offset",
			cards:        []string{"CCD-TEMP=                -15.0", "OFFSET  =                   50"},
			offset:       30,
			offsetSet:    true,
			wantRejected: true,
			want:         []string{"OFFSET=50 (want 30)"},
		},
		{
			name:         "missing offset",
			cards:        []string{"CCD-TEMP=                -15.0"},
			offset:       30,
			offsetSet:    true,
			wantRejected: true,
			want:         []string{"OFFSET missing"},
		},
		{
			name:      "offset ignored without flag",
			cards:     []string{"CCD-TEMP=                -15.0", "OFFSET  =                   50"},
			offset:    30,
			offsetSet: false,
		},
		{
			name: "temperature, gain and offset all fail",
			cards: []string{
				"CCD-TEMP=                -10.0",
				"GAIN    =                  200",
				"OFFSET  =                   50",
			},
			gain:         100,
			gainSet:      true,
			offset:       30,
			offsetSet:    true,
			wantRejected: true,
			want: []string{
				"CCD-TEMP=-10.00 (want -15.00)",
				"GAIN=200 (want 100)",
				"OFFSET=50 (want 30)",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFITS(t, dir, "a.fits", tt.cards...)

			var out bytes.Buffer
			err := runCheck(context.Background(), &out, &checkOptions{
				dir: dir, temp: -15,
				gain: tt.gain, gainSet: tt.gainSet,
				offset: tt.offset, offsetSet: tt.offsetSet,
			})
			if gotErr := errors.Is(err, errRejected); gotErr != tt.wantRejected {
				t.Fatalf("err = %v, want rejected=%v\n%s", err, tt.wantRejected, out.String())
			}
			for _, want := range tt.want {
				if !strings.Contains(out.String(), want) {
					t.Errorf("report missing %q:\n%s", want, out.String())
				}
			}
		})
	}
}

func TestCheckOffsetFlag(t *testing.T) {
	dir := t.TempDir()
	writeFITS(t, dir, "a.fits",
		"CCD-TEMP=                -15.0",
		"OFFSET  =                   30")

	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{
			name: "offset given and matching",
			args: []string{"--dir", dir, "--temp", "-15", "--offset", "30"},
		},
		{
			name:    "offset given and mismatched",
			args:    []string{"--dir", dir, "--temp", "-15", "--offset", "40"},
			wantErr: true,
		},
		{
			name: "offset omitted",
			args: []string{"--dir", dir, "--temp", "-15"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newCheckCmd()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetArgs(tt.args)

			err := cmd.Execute()
			if gotErr := errors.Is(err, errRejected); gotErr != tt.wantErr {
				t.Fatalf("Execute() = %v, want rejected=%v\n%s", err, tt.wantErr, out.String())
			}
		})
	}
}

func TestIsFITS(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"a.fits", true},
		{"a.FIT", true},
		{"a.Fts", true},
		{"a.fits.fz", false},
		{"a.txt", false},
		{"a", false},
		{"._a.fits", false},
		{"/tmp/dir/.hidden.fits", false},
	}
	for _, tt := range tests {
		if got := isFITS(tt.path); got != tt.want {
			t.Errorf("isFITS(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestCheckValidate(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.fits")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		opts    checkOptions
		wantErr bool
	}{
		{name: "valid", opts: checkOptions{dir: dir, temp: -15}},
		{name: "negative tolerance", opts: checkOptions{dir: dir, temp: -15, tolerance: -1}, wantErr: true},
		{name: "missing dir", opts: checkOptions{temp: -15}, wantErr: true},
		{name: "not a directory", opts: checkOptions{dir: file, temp: -15}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("validate() = %v, want error=%v", err, tt.wantErr)
			}
		})
	}
}
