package textenc

import "testing"

func TestDecodeMixed(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want string
	}{
		{"pure utf8 untouched", []byte("Perú, Ñuñoa, Categoría"), "Perú, Ñuñoa, Categoría"},
		{"pure cp1252", []byte("Funci\xf3n P\xfablica"), "Función Pública"},
		{"mixed in one body", []byte("Categor\xc3\xada / Funci\xf3n"), "Categoría / Función"},
		{"cp1252 specials", []byte("\x80 \x93cita\x94"), "€ “cita”"},
		{"empty", nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DecodeMixed(c.in); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}
