package main

import (
	"bytes"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"text/template"
)

// // // // // // // // // //

func writeFileFromTemplate(pathToFile string, textTemplate string, dataTemplate any) error {
	fileName := filepath.Base(pathToFile)

	t, err := template.New(fileName).Parse(textTemplate)
	if err != nil {
		return fmt.Errorf("init template [%s]: %s", fileName, err.Error())
	}

	var buf bytes.Buffer
	err = t.Execute(&buf, dataTemplate)
	if err != nil {
		return fmt.Errorf("filling template [%s]: %s", fileName, err.Error())
	}

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return fmt.Errorf("format template [%s]: %s", fileName, err.Error())
	}

	err = os.MkdirAll(filepath.Dir(pathToFile), 0755)
	if err != nil {
		return fmt.Errorf("create output directory [%s]: %s", filepath.Dir(pathToFile), err.Error())
	}

	err = os.WriteFile(pathToFile, formatted, 0644)
	if err != nil {
		return fmt.Errorf("write file [%s]: %s", fileName, err.Error())
	}

	fmt.Println("\tGenerate: " + pathToFile)
	return nil
}
