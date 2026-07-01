package local_packages_parser

import (
	"fmt"
	"os"

	"github.com/mistweaverco/nvpm-client/internal/lib/files"
)

// FileManager defines the interface for file operations
type FileManager interface {
	GetAppLocalPackagesFilePath() string
	FileExists(path string) bool
	ReadFile(path string) ([]byte, error)
	WriteFile(path string, data []byte, perm uint32) error
}

// LocalPackagesManager defines the interface for local packages operations
type LocalPackagesManager interface {
	GetData(force bool) LocalPackageRoot
	GetDataForProvider(provider string) LocalPackageRoot
	AddLocalPackage(sourceId string, version string) error
	RemoveLocalPackage(sourceId string) error
	GetBySourceId(sourceId string) LocalPackageItem
	IsPackageInstalled(sourceId string) bool
}

// DefaultFileManager implements FileManager using the files package
type DefaultFileManager struct{}

func (dfm *DefaultFileManager) GetAppLocalPackagesFilePath() string {
	return files.GetAppLocalPackagesFilePath()
}

func (dfm *DefaultFileManager) FileExists(path string) bool {
	return files.FileExists(path)
}

func (dfm *DefaultFileManager) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (dfm *DefaultFileManager) WriteFile(path string, data []byte, perm uint32) error {
	return os.WriteFile(path, data, os.FileMode(perm))
}

// MockFileManager is a mock implementation for testing
type MockFileManager struct {
	GetAppLocalPackagesFilePathFunc func() string
	FileExistsFunc                  func(path string) bool
	ReadFileFunc                    func(path string) ([]byte, error)
	WriteFileFunc                   func(path string, data []byte, perm uint32) error
}

func (mfm *MockFileManager) GetAppLocalPackagesFilePath() string {
	if mfm.GetAppLocalPackagesFilePathFunc != nil {
		return mfm.GetAppLocalPackagesFilePathFunc()
	}
	return "/mock/path/local-packages.json"
}

func (mfm *MockFileManager) FileExists(path string) bool {
	if mfm.FileExistsFunc != nil {
		return mfm.FileExistsFunc(path)
	}
	return false
}

func (mfm *MockFileManager) ReadFile(path string) ([]byte, error) {
	if mfm.ReadFileFunc != nil {
		return mfm.ReadFileFunc(path)
	}
	return nil, fmt.Errorf("mock read error")
}

func (mfm *MockFileManager) WriteFile(path string, data []byte, perm uint32) error {
	if mfm.WriteFileFunc != nil {
		return mfm.WriteFileFunc(path, data, perm)
	}
	return nil
}
