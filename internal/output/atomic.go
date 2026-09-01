// Package output writes generated artifacts without exposing partial files
package output

import (
	"fmt"
	"os"
	"path/filepath"
)

// File 表示一个待写入的生成产物
type File struct {
	Path string // 目标路径
	Data []byte // 文件内容
}

type stagedFile struct {
	target string // 目标路径
	temp   string // 临时路径
}

// WriteAtomic 先完整写入同目录临时文件再替换目标
func WriteAtomic(files ...File) error {
	staged := make([]stagedFile, 0, len(files))
	for _, file := range files {
		dir := filepath.Dir(file.Path)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			removeTemporaryFiles(staged)
			return fmt.Errorf("create output directory %s: %w", dir, err)
		}
		temporary, err := os.CreateTemp(dir, "."+filepath.Base(file.Path)+"-*")
		if err != nil {
			removeTemporaryFiles(staged)
			return fmt.Errorf("create temporary output for %s: %w", file.Path, err)
		}
		tempPath := temporary.Name()
		err = temporary.Chmod(0o644)
		if err == nil {
			_, err = temporary.Write(file.Data)
		}
		closeErr := temporary.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			_ = os.Remove(tempPath)
			removeTemporaryFiles(staged)
			return fmt.Errorf("write temporary output for %s: %w", file.Path, err)
		}
		staged = append(staged, stagedFile{target: file.Path, temp: tempPath})
	}

	for index, file := range staged {
		if err := os.Rename(file.temp, file.target); err != nil {
			removeTemporaryFiles(staged[index:])
			return fmt.Errorf("replace output %s: %w", file.target, err)
		}
	}
	return nil
}

// removeTemporaryFiles 清理尚未替换的临时文件
func removeTemporaryFiles(files []stagedFile) {
	for _, file := range files {
		_ = os.Remove(file.temp)
	}
}
