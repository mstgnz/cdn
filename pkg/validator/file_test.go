package validator

import (
	"mime/multipart"
	"net/textproto"
	"strings"
	"testing"
)

// Uploads pass two gates: the extension allowlist and the bytes themselves.
// Neither is client-controlled. The MIME type the client claims was a third
// until 1.10.0, when it was dropped for being a string the caller writes.
//
// The byte check is the one worth testing hardest, from both directions: an
// extension check alone would let anything through under a permitted name, and
// a byte check that is too strict rejects real files under a permitted name,
// which is the failure operators "fix" by turning validation off entirely.

// header builds the multipart header ValidateFile inspects.
func header(filename, contentType string, size int64) *multipart.FileHeader {
	h := make(textproto.MIMEHeader)
	h.Set("Content-Type", contentType)
	return &multipart.FileHeader{
		Filename: filename,
		Header:   h,
		Size:     size,
	}
}

// Magic bytes for the container formats. Office documents and archives are all
// ZIP underneath, and legacy Office files are OLE compound documents.
var (
	zipSig  = []byte{0x50, 0x4B, 0x03, 0x04}
	oleSig  = []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}
	pdfSig  = []byte("%PDF-1.7\n")
	pngSig  = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	riffSig = []byte{0x52, 0x49, 0x46, 0x46}
)

// isoBMFF builds an ISO base media header: a 4-byte big-endian box length, the
// literal "ftyp", then the brand. boxLen is what varies between real encoders
// and what a signature must therefore ignore.
func isoBMFF(boxLen int, brand string) []byte {
	out := []byte{byte(boxLen >> 24), byte(boxLen >> 16), byte(boxLen >> 8), byte(boxLen)}
	out = append(out, 'f', 't', 'y', 'p')
	return append(out, []byte(brand)...)
}

func pad(sig []byte, n int) []byte {
	out := append([]byte{}, sig...)
	for i := 0; i < n; i++ {
		out = append(out, byte(i*7%251))
	}
	return out
}

// The formats this service accepts have to actually survive the content gate.
// A format on the extension allowlist whose bytes are then rejected is worse
// than not supporting it: the upload fails with a message about content while
// the documentation says the extension is fine.
func TestValidateFileContentAcceptsSupportedFormats(t *testing.T) {
	cases := []struct {
		name    string
		content []byte
	}{
		{"docx (zip container)", pad(zipSig, 64)},
		{"xlsx (zip container)", pad(zipSig, 64)},
		{"pptx (zip container)", pad(zipSig, 64)},
		{"zip archive", pad(zipSig, 128)},
		{"doc (ole compound)", pad(oleSig, 64)},
		{"xls (ole compound)", pad(oleSig, 64)},
		{"pdf", pad(pdfSig, 32)},
		{"png", pad(pngSig, 32)},
		{"wav (riff)", pad(riffSig, 32)},
		{"csv", []byte("id,name,total\n1,\"Ünal, Ayşe\",12.50\n2,Bob,3\n")},
		{"csv with only a header", []byte("a,b,c\n")},
		{"sql", []byte("-- dump\nINSERT INTO t VALUES ('ğüş');\n")},

		// ISO base media, one case per box length seen in the wild. The length
		// precedes "ftyp" and is not part of any signature; pinning it here is
		// what previously made real heic/mp4/mov uploads fail with
		// INVALID_FILE_CONTENT while their extension was allowlisted.
		{"heic (ImageMagick, 0x1C)", pad(isoBMFF(0x1C, "heix"), 64)},
		{"heic (0x18)", pad(isoBMFF(0x18, "heic"), 64)},
		{"heif", pad(isoBMFF(0x20, "mif1"), 64)},
		{"avif", pad(isoBMFF(0x1C, "avif"), 64)},
		{"mp4 (ffmpeg, 0x20)", pad(isoBMFF(0x20, "isom"), 64)},
		{"mov (QuickTime, 0x14)", pad(isoBMFF(0x14, "qt  "), 64)},
		{"3gp", pad(isoBMFF(0x14, "3gp4"), 64)},

		// TIFF in both byte orders. It had no entry at all, so the extension was
		// accepted and the bytes were then always refused.
		{"tiff little-endian", pad([]byte{0x49, 0x49, 0x2A, 0x00}, 64)},
		{"tiff big-endian", pad([]byte{0x4D, 0x4D, 0x00, 0x2A}, 64)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateFileContent(tc.content); err != nil {
				t.Fatalf("a supported format was rejected by the content check: %v", err)
			}
		})
	}
}

// The content gate is what stops an executable payload arriving under a
// permitted extension. Binary that matches no known signature and is not text
// has to be refused.
func TestValidateFileContentRejectsUnknownBinary(t *testing.T) {
	cases := []struct {
		name    string
		content []byte
	}{
		{
			// An ELF binary renamed to something allowed.
			name:    "elf executable",
			content: []byte{0x7F, 0x45, 0x4C, 0x46, 0x02, 0x01, 0x01, 0x00, 0x00, 0x00},
		},
		{
			// Null bytes disqualify it as text, and it matches no signature.
			name:    "arbitrary binary with NULs",
			content: []byte{0x11, 0x22, 0x33, 0x44, 0x00, 0x55, 0x66, 0x00, 0x77},
		},
		{
			name:    "invalid utf-8",
			content: []byte{0x41, 0x42, 0xC3, 0x28, 0x43, 0x44, 0x45, 0x46},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateFileContent(tc.content); err == nil {
				t.Fatal("unrecognised binary passed the content check")
			}
		})
	}
}

// A CSV is text, and text is accepted by falling through to the UTF-8 check.
// That is deliberate, but it means a spreadsheet formula in a CSV cell is stored
// verbatim: the risk lives in whatever opens the file later, not here. Pinned so
// the behaviour is a decision rather than a surprise.
func TestValidateFileContentStoresCSVFormulasVerbatim(t *testing.T) {
	csv := []byte("name,amount\n=cmd|' /C calc'!A0,5\n")

	if err := ValidateFileContent(csv); err != nil {
		t.Fatalf("a CSV was rejected: %v", err)
	}
}

// The extension allowlist is the first gate. These are the formats the service
// says it supports, and each has to be accepted with a plausible MIME type.
func TestValidateFileAcceptsSupportedExtensions(t *testing.T) {
	cases := []struct {
		filename    string
		contentType string
	}{
		{"report.doc", "application/msword"},
		{"report.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
		{"data.csv", "text/csv"},
		{"data.csv", "application/vnd.ms-excel"}, // what several browsers actually send
		{"bundle.zip", "application/zip"},
		{"bundle.zip", "application/x-zip-compressed"},
		{"sheet.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
		{"doc.pdf", "application/pdf"},
		{"photo.jpg", "image/jpeg"},
		{"clip.mp4", "video/mp4"},
	}

	for _, tc := range cases {
		t.Run(tc.filename+" as "+tc.contentType, func(t *testing.T) {
			if err := ValidateFile(header(tc.filename, tc.contentType, 1024)); err != nil {
				t.Fatalf("a supported upload was rejected: %v", err)
			}
		})
	}
}

// Extensions that can be executed or interpreted by a browser stay out. This is
// the boundary that makes the service safe to serve user uploads from.
func TestValidateFileRejectsDangerousExtensions(t *testing.T) {
	for _, filename := range []string{
		"shell.php", "page.html", "app.js", "run.sh", "lib.so",
		"tool.exe", "config.xml", "note.txt", "data.json",
	} {
		t.Run(filename, func(t *testing.T) {
			err := ValidateFile(header(filename, "application/octet-stream", 1024))
			if err == nil {
				t.Fatalf("%s was accepted by the extension allowlist", filename)
			}
			if e, ok := err.(*FileValidationError); ok && e.Code != "INVALID_FILE_FORMAT" {
				t.Errorf("expected INVALID_FILE_FORMAT, got %s", e.Code)
			}
		})
	}
}

// A double extension has to be judged on the last one, since that is what a
// server or browser would act on.
func TestValidateFileJudgesTheFinalExtension(t *testing.T) {
	if err := ValidateFile(header("invoice.pdf.php", "application/pdf", 1024)); err == nil {
		t.Fatal("invoice.pdf.php was accepted; only the final extension counts")
	}
	if err := ValidateFile(header("archive.php.zip", "application/zip", 1024)); err != nil {
		t.Fatalf("archive.php.zip should be judged on .zip: %v", err)
	}
}

// Case is not a way around the allowlist.
func TestValidateFileIgnoresExtensionCase(t *testing.T) {
	for _, filename := range []string{"REPORT.DOCX", "Data.Csv", "BUNDLE.Zip"} {
		if err := ValidateFile(header(filename, "application/octet-stream", 1024)); err != nil {
			t.Errorf("%s was rejected: %v", filename, err)
		}
	}
	if err := ValidateFile(header("SHELL.PHP", "application/octet-stream", 1024)); err == nil {
		t.Error("SHELL.PHP was accepted")
	}
}

// An oversized file is refused before anything reads it.
func TestValidateFileRejectsOversizedUpload(t *testing.T) {
	t.Setenv("MAX_FILE_SIZE", "1024")

	err := ValidateFile(header("big.pdf", "application/pdf", 2048))
	if err == nil {
		t.Fatal("an oversized upload was accepted")
	}
	if e, ok := err.(*FileValidationError); ok && e.Code != "FILE_TOO_LARGE" {
		t.Errorf("expected FILE_TOO_LARGE, got %s", e.Code)
	}
}

// VALIDATE_FILE=false is an escape hatch for deployments that trust their
// callers. It has to disable both gates, or it disables neither usefully.
func TestValidationCanBeDisabled(t *testing.T) {
	t.Setenv("VALIDATE_FILE", "false")

	if err := ValidateFile(header("shell.php", "application/x-httpd-php", 1024)); err != nil {
		t.Errorf("extension check still ran with VALIDATE_FILE=false: %v", err)
	}
	if err := ValidateFileContent([]byte{0x7F, 0x45, 0x4C, 0x46, 0x00}); err != nil {
		t.Errorf("content check still ran with VALIDATE_FILE=false: %v", err)
	}
}

// getAllowedFormats feeds the error message a rejected caller sees, so it has to
// actually list the formats rather than being empty or truncated.
func TestAllowedFormatsAreReported(t *testing.T) {
	formats := getAllowedFormats()

	for _, want := range []string{".doc", ".docx", ".csv", ".zip", ".pdf", ".png"} {
		if !strings.Contains(formats, want) {
			t.Errorf("%s is allowed but missing from the error message: %s", want, formats)
		}
	}
}
