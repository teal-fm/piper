package main

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/bluesky-social/indigo/lex"
	"golang.org/x/tools/imports"
)

func main() {
	if err := generateLexicons(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func generateLexicons() error {
	if err := os.RemoveAll("./api"); err != nil {
		return err
	}

	if err := generateLex("./lexcfg.json", "./lexicons"); err != nil {
		return err
	}

	if err := removeInit(); err != nil {
		return err
	}

	if err := fixImports(); err != nil {
		return err
	}

	if err := generateCBOR([]string{
		"teal.AlphaFeedPlay",
		"teal.AlphaActorProfile",
		"teal.AlphaActorStatus",
		"teal.AlphaActorProfile_FeaturedItem",
		"teal.AlphaFeedDefs_PlayView",
		"teal.AlphaFeedDefs_Artist",
	}); err != nil {
		return err
	}

	if err := restoreInit(); err != nil {
		return err
	}

	return nil
}

func generateLex(lexcfgPath, lexiconsPath string) error {
	configData, err := os.ReadFile(lexcfgPath)
	if err != nil {
		return fmt.Errorf("failed to read lexcfg.json: %w", err)
	}

	packages, err := lex.ParsePackages(configData)
	if err != nil {
		return fmt.Errorf("failed to parse lexcfg.json: %w", err)
	}

	schemaPaths, err := findSchemas(lexiconsPath)
	if err != nil {
		return fmt.Errorf("failed to find schemas: %w", err)
	}

	schemas, err := loadSchemas(schemaPaths)
	if err != nil {
		return err
	}

	if err := lex.Run(schemas, nil, packages); err != nil {
		return fmt.Errorf("failed to generate code: %w", err)
	}

	return nil
}

func findSchemas(lexiconsPath string) ([]string, error) {
	var paths []string

	err := filepath.Walk(lexiconsPath, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		if !strings.HasSuffix(path, ".json") {
			return nil
		}

		paths = append(paths, path)
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk: %w", err)
	}

	return paths, nil
}

func loadSchemas(paths []string) ([]*lex.Schema, error) {
	var schemas []*lex.Schema

	for _, path := range paths {
		s, err := lex.ReadSchema(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read schema %s: %w", path, err)
		}

		schemas = append(schemas, s)
	}

	return schemas, nil
}

func removeInit() error {
	files, err := filepath.Glob("./api/teal/*.go")
	if err != nil {
		return fmt.Errorf("failed to glob teal files: %w", err)
	}

	for _, file := range files {
		if err := commentOutUtilInFile(file); err != nil {
			return err
		}
	}

	return nil
}

func commentOutUtilInFile(file string) error {
	content, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", file, err)
	}

	if err := os.WriteFile(file+".bak", content, 0644); err != nil {
		return fmt.Errorf("failed to write backup %s: %w", file, err)
	}

	modified := strings.ReplaceAll(string(content), "\tutil", "//\tutil")

	if err := os.WriteFile(file, []byte(modified), 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", file, err)
	}

	return nil
}

func fixImports() error {
	files, err := filepath.Glob("./api/teal/*.go")
	if err != nil {
		return fmt.Errorf("failed to glob teal files: %w", err)
	}

	for _, file := range files {
		if err := formatFile(file); err != nil {
			return err
		}
	}

	return nil
}

func formatFile(file string) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", file, err)
	}

	formatted, err := imports.Process(file, data, nil)
	if err != nil {
		return fmt.Errorf("failed to format %s: %w", file, err)
	}

	if err := os.WriteFile(file, formatted, 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", file, err)
	}

	return nil
}

func generateCBOR(types []string) error {
	var typeArgs strings.Builder
	for _, t := range types {
		fmt.Fprintf(&typeArgs, "\t\t%s{},\n", t)
	}

	code := fmt.Sprintf(`package main

import (
	"github.com/teal-fm/piper/api/teal"
	cbg "github.com/whyrusleeping/cbor-gen"
)

func main() {
	genCfg := cbg.Gen{
		MaxStringLength: 1_000_000,
	}

	if err := genCfg.WriteMapEncodersToFile(
		"api/teal/cbor_gen.go",
		"teal",
%s	); err != nil {
		panic(err)
	}
}
`, typeArgs.String())

	tmpDir, err := os.MkdirTemp("", "gencbor-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	mainPath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(mainPath, []byte(code), 0644); err != nil {
		return fmt.Errorf("failed to write main.go: %w", err)
	}

	cmd := exec.Command("go", "run", mainPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to run CBOR generator: %w", err)
	}

	return nil
}

func restoreInit() error {
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		if !strings.HasSuffix(path, ".bak") {
			return nil
		}

		if err := os.Rename(path, strings.TrimSuffix(path, ".bak")); err != nil {
			return fmt.Errorf("failed to remove %s: %w", path, err)
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to clean backup files: %w", err)
	}

	return nil
}
