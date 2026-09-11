// Package textenc repairs the character encodings MEF services emit.
//
// Consulta Amigable declares `charset=utf-8` but splices Windows-1252 bytes
// from its database into otherwise valid UTF-8 markup, so one response holds
// both "Categoría" (UTF-8) and "Funci\xf3n" (cp1252). Decoding the whole body
// as either charset corrupts half of it; decoding byte-run by byte-run fixes
// both.
package textenc

import (
	"strings"
	"unicode/utf8"
)

// cp1252 maps bytes 0x80–0x9F, where Windows-1252 differs from Latin-1.
var cp1252 = [32]rune{
	'€', 0xFFFD, '‚', 'ƒ', '„', '…', '†', '‡', 'ˆ', '‰', 'Š', '‹', 'Œ', 0xFFFD, 'Ž', 0xFFFD,
	0xFFFD, '‘', '’', '“', '”', '•', '–', '—', '˜', '™', 'š', '›', 'œ', 0xFFFD, 'ž', 'Ÿ',
}

// DecodeMixed keeps every valid UTF-8 sequence and reinterprets each invalid
// byte as Windows-1252. Pure UTF-8 input is returned unchanged.
func DecodeMixed(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	var sb strings.Builder
	sb.Grow(len(b) + len(b)/8)
	for len(b) > 0 {
		r, size := utf8.DecodeRune(b)
		if r == utf8.RuneError && size <= 1 {
			sb.WriteRune(fromCP1252(b[0]))
			b = b[1:]
			continue
		}
		sb.WriteRune(r)
		b = b[size:]
	}
	return sb.String()
}

func fromCP1252(c byte) rune {
	if c >= 0x80 && c <= 0x9F {
		return cp1252[c-0x80]
	}
	return rune(c) // 0xA0–0xFF coincide with Latin-1 / Unicode
}
