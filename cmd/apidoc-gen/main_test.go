package main

import (
	"bytes"
	"runtime/debug"
	"strings"
	"testing"
)

// TestRunHelp 验证 CLI 帮助包含所有对外命令
func TestRunHelp(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exitCode := run([]string{"help"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("run() exit code = %d, want 0", exitCode)
	}
	for _, command := range []string{"init", "generate", "validate", "version"} {
		if !strings.Contains(stdout.String(), command) {
			t.Errorf("help output does not contain %q", command)
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

// TestRunVersion 验证开发版本输出
func TestRunVersion(t *testing.T) {
	var stdout bytes.Buffer
	if exitCode := run([]string{"version"}, &stdout, &bytes.Buffer{}); exitCode != 0 {
		t.Fatalf("run() exit code = %d, want 0", exitCode)
	}
	if got, want := stdout.String(), "apidoc-gen dev\n"; got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
}

// TestBuildVersion 验证发布安装时输出 Go Build Info 中的 Module 标签
func TestBuildVersion(t *testing.T) {
	info := &debug.BuildInfo{Main: debug.Module{Version: "v0.1.0"}}
	if got := buildVersion(info, true); got != "v0.1.0" {
		t.Fatalf("buildVersion() = %q, want v0.1.0", got)
	}
	if got := buildVersion(&debug.BuildInfo{}, true); got != "dev" {
		t.Fatalf("buildVersion() = %q, want dev", got)
	}
}

// TestRemovedCommands 验证旧命令及配置参数已不可用
func TestRemovedCommands(t *testing.T) {
	for _, args := range [][]string{{"list"}, {"explain"}, {"list-unresolved"}, {"check-overrides"}, {"generate", "--config", "old.yaml"}} {
		var out, err bytes.Buffer
		if code := run(args, &out, &err); code != 2 {
			t.Fatalf("%v: exit %d, stderr %s", args, code, err.String())
		}
	}
}
