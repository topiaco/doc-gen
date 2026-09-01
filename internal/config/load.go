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
	"golang.org/x/mod/modfile"
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
	if !pathInside(root, paths.Config) {
		return nil, fmt.Errorf("configuration file must be inside project root: %s", paths.Config)
	}
	var cfg Config
	if err := decodeFile(paths.Config, "config-v2.json", &cfg); err != nil {
		return nil, err
	}
	if err := validateProject(root, cfg); err != nil {
		return nil, err
	}

	base := filepath.Dir(paths.Config)
	filePaths := []struct {
		name   string
		value  string
		target *string
	}{
		{"selection", cfg.Files.Selection, &paths.Selection},
		{"overrides", cfg.Files.Overrides, &paths.Overrides},
		{"output", cfg.Files.Output, &paths.Output},
		{"report", cfg.Files.Report, &paths.Report},
	}
	for _, file := range filePaths {
		resolved, err := resolveProjectPath(root, base, file.value)
		if err != nil {
			return nil, fmt.Errorf("files.%s: %w", file.name, err)
		}
		*file.target = resolved
	}

	var selection Selection
	if err := decodeFile(paths.Selection, "selection-v2.json", &selection); err != nil {
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
		if err := decodeFile(path, "override-v2.json", &document); err != nil {
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
	var header struct {
		ConfigVersion string `yaml:"configVersion"`
	}
	if err := yaml.Unmarshal(data, &header); err != nil {
		return fmt.Errorf("decode config %s: %w", path, err)
	}
	if header.ConfigVersion != "v2" {
		return fmt.Errorf("config %s uses unsupported configVersion %q; expected v2", path, header.ConfigVersion)
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

// resolveProjectPath 解析配置目录相对路径并限制在项目根目录内
func resolveProjectPath(root, base, path string) (string, error) {
	if filepath.IsAbs(path) {
		return "", fmt.Errorf("path must be relative: %s", path)
	}
	resolved := filepath.Join(base, filepath.Clean(path))
	if !pathInside(root, resolved) {
		return "", fmt.Errorf("path escapes project root: %s", path)
	}
	return resolved, nil
}

func pathInside(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && filepath.IsLocal(relative)
}

// validateProject 校验项目 Module、包范围和响应配置
func validateProject(root string, cfg Config) error {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return fmt.Errorf("read project go.mod: %w", err)
	}
	if module := modfile.ModulePath(data); module != cfg.Project.Module {
		return fmt.Errorf("project.module %q does not match go.mod module %q", cfg.Project.Module, module)
	}
	for _, pattern := range append(append([]string{}, cfg.Source.RoutePackages...), cfg.Source.AnalysisPackages...) {
		if filepath.IsAbs(pattern) || !filepath.IsLocal(filepath.Clean(pattern)) {
			return fmt.Errorf("source package pattern must be project-relative: %s", pattern)
		}
	}
	for name, wrapper := range cfg.ResponseWrappers {
		properties, ok := wrapper.Schema["properties"].(map[string]any)
		if !ok || properties[wrapper.DataProperty] == nil {
			return fmt.Errorf("responseWrappers.%s dataProperty %q is not defined in schema properties", name, wrapper.DataProperty)
		}
	}
	for _, function := range cfg.ResponseFunctions {
		if (function.Status != 0) == (function.StatusArgument != nil) {
			return fmt.Errorf("response function %s must define exactly one of status or statusArgument", function.Symbol)
		}
		if function.Kind == "binary" {
			if function.Wrapper != "" || function.DataArgument != nil || function.DataTarget {
				return fmt.Errorf("binary response function %s cannot define JSON response fields", function.Symbol)
			}
			continue
		}
		if function.Wrapper != "" {
			if _, found := cfg.ResponseWrappers[function.Wrapper]; !found {
				return fmt.Errorf("response function %s references unknown wrapper %q", function.Symbol, function.Wrapper)
			}
		}
		if function.DataTarget && function.DataArgument == nil {
			return fmt.Errorf("response function %s dataTarget requires dataArgument", function.Symbol)
		}
	}
	return nil
}
