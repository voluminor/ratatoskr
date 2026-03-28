package _generate

import (
	"bytes"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"unicode"
	"unicode/utf8"
)

// // // // // // // // // //

func WriteFileFromTemplate(pathToFile string, textTemplate string, dataTemplate any) error {
	fileName := filepath.Base(pathToFile)

	tmpl := template.New("cli-template").Funcs(template.FuncMap{
		"split": strings.Split,
		"mod":   func(a, b int) int { return a % b },
	})

	t, err := tmpl.New(fileName).Parse(textTemplate)
	if err != nil {
		return fmt.Errorf("init template [%s]: %s", fileName, err.Error())
	}

	var buf bytes.Buffer
	err = t.Execute(&buf, dataTemplate)
	if err != nil {
		return fmt.Errorf("filling template [%s]: %s", fileName, err.Error())
	}

	file, err := os.OpenFile(pathToFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("open file [%s]: %s", fileName, err.Error())
	}
	defer file.Close()

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		fmt.Printf("ERROR\tformat template [%s]: %s\n", fileName, err.Error())
		formatted = buf.Bytes()
	}

	_, err = file.Write(formatted)
	if err != nil {
		return fmt.Errorf("write file [%s]: %s", fileName, err.Error())
	}

	fmt.Println("\tGenerate: " + pathToFile)
	return nil
}

// //

func GenGoName(p string) string {
	var tokens []string
	var tok strings.Builder

	flush := func() {
		if tok.Len() > 0 {
			tokens = append(tokens, tok.String())
			tok.Reset()
		}
	}

	for _, r := range p {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			tok.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()

	if len(tokens) == 0 {
		return "N"
	}

	var out strings.Builder
	for _, t := range tokens {
		out.WriteString(pascalizeToken(t))
	}

	s := out.String()
	if first, _ := utf8.DecodeRuneInString(s); unicode.IsDigit(first) {
		s = "N" + s
	}
	return s
}

func pascalizeToken(t string) string {
	if t == "" {
		return ""
	}
	i := 0
	for i < len(t) {
		r, sz := utf8.DecodeRuneInString(t[i:])
		if !unicode.IsDigit(r) {
			break
		}
		i += sz
	}

	var b strings.Builder
	if i > 0 {
		b.WriteString(t[:i])
	}

	rest := t[i:]
	if rest == "" {
		return b.String()
	}

	r, sz := utf8.DecodeRuneInString(rest)
	b.WriteRune(unicode.ToUpper(r))
	for _, r2 := range rest[sz:] {
		b.WriteRune(unicode.ToLower(r2))
	}
	return b.String()
}
