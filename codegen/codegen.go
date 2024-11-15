package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/palantir/stacktrace"
)

type CodegenDef struct {
	Package       string   `json:"package"`
	OutputFile    string   `json:"outputFile"`
	OpaqueStrings []string `json:"opaqueStrings"`
}

func main() {
	err := filepath.Walk(".",
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if strings.HasSuffix(path, "opaque-types.json") {
				path, err := filepath.Abs(path)
				if err != nil {
					return stacktrace.Propagate(err, "failed to filepath.Abs %s", path)
				}
				err = processOpaqueTypesJson(path)
				if err != nil {
					stacktrace.Propagate(err, "error processing %s", path)
				}
			}
			return nil
		})
	if err != nil {
		log.Println(err)
	}
}

func processOpaqueTypesJson(path string) error {

	file, err := os.Open(path)
	if err != nil {
		return err
	}

	codegenJson, err := io.ReadAll(file)
	if err != nil {
		return err
	}

	var codegenDef CodegenDef
	err = json.Unmarshal(codegenJson, &codegenDef)
	if err != nil {
		return err
	}

	workingDir := filepath.Dir(path)

	RunCodegen(codegenDef, workingDir)

	return nil

}

func RunCodegen(codegenDef CodegenDef, workingDir string) {

	packageName := codegenDef.Package
	outputFile := codegenDef.OutputFile

	lines := []string{
		"package " + packageName,
		"",
	}

	for _, opaqueString := range codegenDef.OpaqueStrings {
		lines = append(lines, OpaqueStringCode(opaqueString)...)
	}

	code := strings.Join(lines, "\n")

	outputPath := filepath.Join(workingDir, outputFile)

	err := os.WriteFile(outputPath, []byte(code), 0644)
	if err != nil {
		log.Fatalf("failed to write file %s: %v", outputPath, err)
	}
}

func OpaqueStringCode(typeName string) []string {

	typeNamePublic := typeName
	typeNamePrivate := "_" + typeNamePublic

	lines := []string{
		"",
		"",
		fmt.Sprintf("type %s interface {", typeNamePublic),
		"    String() string",
		"    IsEmpty() bool",
		"    implementsOpaque()",
		"}",
		"",
		fmt.Sprintf("type %s string", typeNamePrivate),
		"",
		fmt.Sprintf("func (value *%s) implementsOpaque() {}", typeNamePrivate),
		"",
		fmt.Sprintf("func (value *%s) String() string {", typeNamePrivate),
		"	return string(*value)",
		"}",
		"",
		fmt.Sprintf("func (value *%s) IsEmpty() bool {", typeNamePrivate),
		"	return len(string(*value)) == 0",
		"}",
		fmt.Sprintf("func New%s(s string) %s {", typeNamePublic, typeNamePublic),
		fmt.Sprintf("	value := %s(s)", typeNamePrivate),
		"	return &value",
		"}",
		"",
		"",
	}

	return lines

}
