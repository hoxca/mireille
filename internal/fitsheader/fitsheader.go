// Package fitsheader reads cards from the primary header of a FITS file
// without decoding its pixel data.
package fitsheader

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// cardSize is the length of one FITS header card in bytes.
const cardSize = 80

// ReadFloat returns the value of the first numeric card named keyword in the
// primary header of the FITS file at path. found is false when the header has
// no such card.
func ReadFloat(path, keyword string) (value float64, found bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false, err
	}
	defer func() { _ = f.Close() }()

	return readFloat(bufio.NewReader(f), keyword)
}

func readFloat(r io.Reader, keyword string) (float64, bool, error) {
	var card [cardSize]byte
	for {
		if _, err := io.ReadFull(r, card[:]); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return 0, false, fmt.Errorf("header ends before the END card")
			}
			return 0, false, err
		}

		name := strings.TrimSpace(string(card[:8]))
		if name == "END" {
			return 0, false, nil
		}
		if !strings.EqualFold(name, keyword) {
			continue
		}

		raw, err := cardValue(card[:])
		if err != nil {
			return 0, false, fmt.Errorf("%s: %w", keyword, err)
		}
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return 0, false, fmt.Errorf("%s: %q is not a number", keyword, raw)
		}
		return v, true, nil
	}
}

// cardValue extracts the value field of a card with the "= " indicator,
// dropping the trailing comment and normalising FITS D exponents.
func cardValue(card []byte) (string, error) {
	if card[8] != '=' {
		return "", fmt.Errorf("card has no value")
	}

	raw := string(card[10:])
	if i := strings.IndexByte(raw, '/'); i >= 0 {
		raw = raw[:i]
	}
	raw = strings.TrimSpace(raw)

	if raw == "" {
		return "", fmt.Errorf("card has an empty value")
	}
	if raw[0] == '\'' {
		return "", fmt.Errorf("value is a string, not a number")
	}

	return strings.NewReplacer("D", "E", "d", "e").Replace(raw), nil
}
