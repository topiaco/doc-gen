package config

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

//go:embed schemas/*.json
var schemaFiles embed.FS

// Load 加载并校验项目 API 文档配置
func Load(projectRoot, configPath string) (*Bundle, error) {
	root, err := filepath.Abs(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve project root: %w", err)
	}

	paths := Paths{ProjectRoot: root, Config: resolvePath(root, configPath)}
	var cfg Config
	if err := decodeFile(paths.Config, "config-v1.json", &cfg); err != nil {
		return nil, err
	}

	paths.Selection = resolvePath(root, cfg.Files.Selection)
	paths.Overrides = resolvePath(root, cfg.Files.Overrides)
	paths.Output = resolvePath(root, cfg.Files.Output)
	paths.Report = resolvePath(root, cfg.Files.Report)

	var selection Selection
	if err := decodeFile(paths.Selection, "selection-v1.json", &selection); err != nil {
		return nil, err
	}

	overrides, err := loadOverrides(paths.Overrides)
	if err != nil {
		return nil, err
	}

	return &Bundle{
		Config:    cfg,
		Selection: selection,
		Overrides: overrides,
		Paths:     paths,
	}, nil
}

// loadOverrides 按文件名顺序加载 Override 配置
func loadOverrides(dir string) ([]OverrideFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read overrides directory %s: %w", dir, err)
	}

	var paths []string
	for _, entry := range entries {
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if !entry.IsDir() && (ext == ".yaml" || ext == ".yml") {
			paths = append(paths, filepath.Join(dir, entry.Name()))
		}
	}
	sort.Strings(paths)

	overrides := make([]OverrideFile, 0, len(paths))
	for _, path := range paths {
		var document OverrideDocument
		if err := decodeFile(path, "override-v1.json", &document); err != nil {
			return nil, err
		}
		overrides = append(overrides, OverrideFile{Path: path, Document: document})
	}
	return overrides, nil
}

// decodeFile 严格解码单个 YAML 文件并执行 JSON Schema 校验
func decodeFile(path, schemaName string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config %s: %w", path, err)
	}

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode config %s: %w", path, err)
	}

	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("decode config %s: multiple YAML documents are not supported", path)
	}

	if err := validateSchema(schemaName, target); err != nil {
		return fmt.Errorf("validate config %s: %w", path, err)
	}
	return nil
}

// validateSchema 使用内嵌 JSON Schema 校验配置结构
func validateSchema(name string, document any) error {
	data, err := schemaFiles.ReadFile("schemas/" + name)
	if err != nil {
		return err
	}

	schemaDocument, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return err
	}
	compiler := jsonschema.NewCompiler()
	uri := "https://apidoc.local/schema/" + name
	if err := compiler.AddResource(uri, schemaDocument); err != nil {
		return err
	}
	schema, err := compiler.Compile(uri)
	if err != nil {
		return err
	}

	encoded, err := json.Marshal(document)
	if err != nil {
		return err
	}
	var value any
	if err := json.Unmarshal(encoded, &value); err != nil {
		return err
	}
	return schema.Validate(value)
}

// resolvePath 将配置路径解析为项目内的绝对路径
func resolvePath(root, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(root, filepath.Clean(path))
}
