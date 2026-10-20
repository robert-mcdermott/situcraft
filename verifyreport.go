package main

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// A report (wave 8, slice 7; web/lib/report.js) given to "situcraft verify": the certificate it
// carries verified as any certificate, and the report's own numbers, figures and methods checked
// against it as the report checks them as it opens (web/lib/report-check.js): each number the
// value at its place in certificate.json and its text that value's as the report writes it, each
// figure's SVG the one certified (its SHA-256), each paragraph of the methods the certificate's.

var (
	reportCarried   = regexp.MustCompile(`<script type="application/zip" id="situcraft-certificate">([A-Za-z0-9+/=\s]*)</script>`)
	reportNumber    = regexp.MustCompile(`data-pointer="([^"]*)" data-value="([^"]*)"[^>]*>([^<]*)<`)
	reportFigure    = regexp.MustCompile(`<img data-figure="([^"]*)"[^>]*? src="data:image/svg\+xml;base64,([A-Za-z0-9+/=]*)"`)
	reportParagraph = regexp.MustCompile(`<p class="methods-paragraph">([^<]*)</p>`)
)

// submitted reads what verify was given: a certificate (its zip's bytes), a report (its page), or a
// result package (wave 8, slice 8: its folder, or a zip of it, whose index.html is its report).
// Answers the report's page (nil for a certificate) or the certificate's bytes.
func submitted(path string) ([]byte, []byte, error) {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		page, err := os.ReadFile(filepath.Join(path, "index.html"))
		if err != nil || !isReport(page) {
			return nil, nil, fmt.Errorf("%s holds no report (index.html): not a SituCraft package", filepath.Base(path))
		}
		return page, nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	if isReport(raw) {
		return raw, nil, nil
	}
	z, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, nil, fmt.Errorf("%s is not a SituCraft certificate: %v", filepath.Base(path), err)
	}
	var index *zip.File
	for _, f := range z.File {
		if f.Name == "certificate.json" {
			return nil, raw, nil
		}
		if dir, rest, _ := strings.Cut(f.Name, "/"); f.Name == "index.html" || (dir != "" && rest == "index.html") {
			index = f
		}
	}
	if index == nil {
		return nil, raw, nil
	}
	r, err := index.Open()
	if err != nil {
		return nil, nil, err
	}
	defer r.Close()
	page, err := io.ReadAll(io.LimitReader(r, 1<<30))
	if err != nil || !isReport(page) {
		return nil, nil, fmt.Errorf("%s: its index.html is not a SituCraft report", filepath.Base(path))
	}
	return page, nil, nil
}

// isReport says whether a file is HTML (a report) rather than a zip.
func isReport(data []byte) bool {
	head := bytes.ToLower(bytes.TrimSpace(data[:min(len(data), 64)]))
	return bytes.HasPrefix(head, []byte("<!doctype html")) || bytes.HasPrefix(head, []byte("<html"))
}

// carriedCertificate answers the certificate a report carries: its zip's bytes.
func carriedCertificate(page []byte) ([]byte, error) {
	m := reportCarried.FindSubmatch(page)
	if m == nil {
		return nil, errors.New("it carries no certificate")
	}
	zip, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(string(m[1])), ""))
	if err != nil {
		return nil, fmt.Errorf("the certificate it carries cannot be read: %v", err)
	}
	return zip, nil
}

// checkReport compares a report's numbers, figures and methods with the certificate it carries
// (its entries read). Answers what it checked, in words, and what is not the certificate's.
func checkReport(page []byte, c *certificate, files map[string][]byte) (string, []string) {
	var problems []string
	doc, err := decodeGeneric(files["certificate.json"])
	if err != nil {
		return "", []string{"certificate.json cannot be read"}
	}
	numbers, differ := 0, 0
	for _, m := range reportNumber.FindAllSubmatch(page, -1) {
		numbers++
		value, err := decodeGeneric([]byte(html.UnescapeString(string(m[2]))))
		at, ok := atPointer(doc, html.UnescapeString(string(m[1])))
		number, isNumber := at.(json.Number)
		if err != nil || !ok || !isNumber || !bytes.Equal(canonicalJSON(value), canonicalJSON(at)) || html.UnescapeString(string(m[3])) != numberText(number) {
			differ++
		}
	}
	if differ > 0 {
		problems = append(problems, fmt.Sprintf("%d of its %d numbers %s not the certificate's", differ, numbers, isAre(differ)))
	}
	steps := map[string]certStep{}
	for _, s := range c.Steps {
		steps[s.ID] = s
	}
	figures, figuresDiffer := 0, 0
	for _, m := range reportFigure.FindAllSubmatch(page, -1) {
		figures++
		svg, err := base64.StdEncoding.DecodeString(string(m[2]))
		if certified := certifiedSVG(steps[html.UnescapeString(string(m[1]))]); err != nil || certified == "" || sha256Hex(svg) != certified {
			figuresDiffer++
		}
	}
	if figuresDiffer > 0 {
		problems = append(problems, fmt.Sprintf("%d of its %d figures %s not the one certified", figuresDiffer, figures, isAre(figuresDiffer)))
	}
	var shown []string
	for _, m := range reportParagraph.FindAllSubmatch(page, -1) {
		shown = append(shown, html.UnescapeString(string(m[1])))
	}
	methods := "no methods"
	if len(shown) > 0 {
		methods = "its methods"
		var certified []string
		text, _, _ := strings.Cut(string(files[c.Methods.Path]), "\n## References")
		for _, line := range strings.Split(text, "\n") {
			if line != "" && !strings.HasPrefix(line, "#") {
				certified = append(certified, line)
			}
		}
		if strings.Join(shown, "\n") != strings.Join(certified, "\n") || len(shown) != len(certified) {
			problems = append(problems, "its methods are not the certificate's")
		}
	}
	return fmt.Sprintf("%s, %s and %s", countOf(numbers, "number"), countOf(figures, "figure"), methods), problems
}

// certifiedSVG answers the SHA-256 of a figure's SVG as a report's certificate holds it ("" without
// one).
func certifiedSVG(s certStep) string {
	var drawing struct {
		SVG string `json:"svg"`
	}
	if len(s.Drawing) == 0 || json.Unmarshal(s.Drawing, &drawing) != nil {
		return ""
	}
	return drawing.SVG
}

// numberText writes a number as the report's tables do (web/lib/report-check.js's numberText, as
// JavaScript writes it): whole numbers with commas; others to 4 significant digits, below 0.001
// and from a million up as 3 significant digits and a power of ten ("1.23e-5"). The number is the
// certificate's text of it, as JSON.stringify wrote it.
func numberText(n json.Number) string {
	v, err := strconv.ParseFloat(string(n), 64)
	if err != nil || math.IsInf(v, 0) || math.IsNaN(v) {
		return string(n)
	}
	if v == math.Trunc(v) {
		// Its shortest digits, as toLocaleString writes a whole number, then zeros.
		mantissa, expText, _ := strings.Cut(strconv.FormatFloat(math.Abs(v), 'e', -1, 64), "e")
		exp, _ := strconv.Atoi(expText)
		digits := strings.Replace(mantissa, ".", "", 1)
		digits += strings.Repeat("0", exp+1-len(digits))
		var b strings.Builder
		if v < 0 {
			b.WriteByte('-')
		}
		for i, c := range digits {
			if i > 0 && (len(digits)-i)%3 == 0 {
				b.WriteByte(',')
			}
			b.WriteRune(c)
		}
		return b.String()
	}
	sign := ""
	if v < 0 {
		sign = "-"
	}
	a := math.Abs(v)
	if a < 1e-3 || a >= 1e6 {
		digits, exp := roundedDigits(a, 3)
		e := strconv.Itoa(exp)
		if exp >= 0 {
			e = "+" + e
		}
		return sign + digits[:1] + "." + digits[1:] + "e" + e
	}
	digits, exp := roundedDigits(a, 4)
	r, _ := strconv.ParseFloat(digits[:1]+"."+digits[1:]+"e"+strconv.Itoa(exp), 64)
	return sign + strconv.FormatFloat(r, 'f', -1, 64)
}

// roundedDigits answers a positive number's first n significant digits, rounded from its exact
// value with a tie away from zero (as JavaScript's toPrecision and toExponential round), and the
// power of ten of the first.
func roundedDigits(a float64, n int) (string, int) {
	// The exact value: a double's decimal expansion has at most 767 significant digits.
	text := new(big.Float).SetFloat64(a).Text('e', 800)
	mantissa, expText, _ := strings.Cut(text, "e")
	exp, _ := strconv.Atoi(expText)
	all := strings.Replace(mantissa, ".", "", 1)
	digits := []byte(all[:n])
	if all[n] >= '5' {
		i := n - 1
		for ; i >= 0 && digits[i] == '9'; i-- {
			digits[i] = '0'
		}
		if i < 0 {
			digits = append([]byte{'1'}, digits[:n-1]...)
			exp++
		} else {
			digits[i]++
		}
	}
	return string(digits), exp
}

// atPointer answers the value at a JSON pointer (RFC 6901) in a decoded document.
func atPointer(doc any, pointer string) (any, bool) {
	if pointer == "" {
		return doc, true
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, false
	}
	v := doc
	for _, part := range strings.Split(pointer[1:], "/") {
		key := strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		switch x := v.(type) {
		case map[string]any:
			next, ok := x[key]
			if !ok {
				return nil, false
			}
			v = next
		case []any:
			i, err := strconv.Atoi(key)
			if err != nil || i < 0 || i >= len(x) {
				return nil, false
			}
			v = x[i]
		default:
			return nil, false
		}
	}
	return v, true
}

func isAre(n int) string {
	if n == 1 {
		return "is"
	}
	return "are"
}

func countOf(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}
