package main

import (
	"bytes"
	"errors"
	"log/slog"
	"reflect"
	"testing"
)

// MockArchiveReader はテスト用のモックです。
type MockArchiveReader struct {
	Entries []FileEntry
	Err     error
}

func (m MockArchiveReader) ReadEntries(path string) ([]FileEntry, error) {
	return m.Entries, m.Err
}

// AggregateFolders のテスト (C0/C1網羅を目指す)
func TestAggregateFolders(t *testing.T) {
	tests := []struct {
		name           string
		entries        []FileEntry
		threshold      int
		expectedResult []FolderCount
		expectedTotal  int
	}{
		{
			name: "正常系：複数ファイルとフォルダの混在",
			entries: []FileEntry{
				{Name: "dir1/", IsDir: true},
				{Name: "dir1/file1.txt", IsDir: false},
				{Name: "dir1/file2.txt", IsDir: false},
				{Name: "dir2/file3.txt", IsDir: false},
				{Name: "file4.txt", IsDir: false}, // Root
			},
			threshold: 1, // 全て抽出
			expectedResult: []FolderCount{
				{Path: "dir1", Count: 2},
				{Path: "(Root)", Count: 1},
				{Path: "dir2", Count: 1},
			},
			expectedTotal: 4,
		},
		{
			name: "境界値：しきい値によるフィルタリングとソートの安定性",
			entries: []FileEntry{
				{Name: "alpha/1.txt", IsDir: false},
				{Name: "beta/1.txt", IsDir: false},
				{Name: "beta/2.txt", IsDir: false},
				{Name: "gamma/1.txt", IsDir: false}, // 閾値未満になる
			},
			threshold: 2,
			expectedResult: []FolderCount{
				{Path: "beta", Count: 2},
			},
			expectedTotal: 4,
		},
		{
			name:           "異常系：空の入力",
			entries:        []FileEntry{},
			threshold:      1,
			expectedResult: nil,
			expectedTotal:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, total := AggregateFolders(tt.entries, tt.threshold)
			if total != tt.expectedTotal {
				t.Errorf("expected total %d, got %d", tt.expectedTotal, total)
			}
			if !reflect.DeepEqual(result, tt.expectedResult) {
				t.Errorf("expected %v, got %v", tt.expectedResult, result)
			}
		})
	}
}

// App.Run のテスト (外部依存の注入とフローの検証)
func TestAppRun(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)) // ログ出力を破棄

	t.Run("異常系：ZipPathが空の場合はエラー", func(t *testing.T) {
		app := &App{Logger: logger}
		err := app.Run(AppConfig{}, bytes.NewBuffer(nil))
		if err == nil || err.Error() != "zip path is required" {
			t.Errorf("expected 'zip path is required' error, got %v", err)
		}
	})

	t.Run("異常系：Readerがエラーを返す場合", func(t *testing.T) {
		app := &App{
			Reader: MockArchiveReader{Err: errors.New("mock read error")},
			Logger: logger,
		}
		err := app.Run(AppConfig{ZipPath: "dummy.zip"}, bytes.NewBuffer(nil))
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("正常系：標準出力へのルーティング", func(t *testing.T) {
		app := &App{
			Reader: MockArchiveReader{
				Entries: []FileEntry{{Name: "test.txt", IsDir: false}},
			},
			Logger: logger,
		}
		outStream := new(bytes.Buffer)
		err := app.Run(AppConfig{ZipPath: "dummy.zip", Threshold: 1}, outStream)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !bytes.Contains(outStream.Bytes(), []byte("(Root)")) {
			t.Errorf("output does not contain expected text, got: %s", outStream.String())
		}
	})
}
