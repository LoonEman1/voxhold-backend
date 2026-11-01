package attachments

import "testing"

func TestSanitizeFilenameRemovesSpoofingCharacters(t *testing.T) {
	cases := map[string]string{
		"photo‮gpj.exe": "photogpj.exe",
		"re​port.pdf":   "report.pdf",
		"a⁦b⁩.txt": "ab.txt",
		"  plain name.txt ":  "plain name.txt",
		"Отчёт.docx":         "Отчёт.docx",
		"‮":             "",
	}
	for in, want := range cases {
		if got := sanitizeFilename(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}
