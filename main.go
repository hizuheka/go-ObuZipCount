package main

import (
	"archive/zip"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
)

// FolderCount はフォルダの情報を保持します。
type FolderCount struct {
	Path  string
	Count int
}

// FileEntry はアーカイブ内のエントリ情報を抽象化します。
type FileEntry struct {
	Name  string
	IsDir bool
}

// =====================================================================
// Domain / Pure Functions (ビジネスロジック)
// =====================================================================

// AggregateFolders はファイルエントリのリストを集計し、しきい値以上のものを抽出・ソートします。(純粋関数)
func AggregateFolders(entries []FileEntry, threshold int) ([]FolderCount, int) {
	counts := make(map[string]int)
	processedFiles := 0

	for _, f := range entries {
		if f.IsDir {
			continue
		}
		processedFiles++

		dirPath := path.Dir(f.Name)
		if dirPath == "." {
			dirPath = "(Root)"
		} else {
			dirPath = strings.ReplaceAll(dirPath, "/", "\\")
		}
		counts[dirPath]++
	}

	var results []FolderCount
	for k, v := range counts {
		if v >= threshold {
			results = append(results, FolderCount{Path: k, Count: v})
		}
	}

	// 件数の降順、件数が同じ場合はパスの昇順で安定ソート
	sort.Slice(results, func(i, j int) bool {
		if results[i].Count == results[j].Count {
			return results[i].Path < results[j].Path
		}
		return results[i].Count > results[j].Count
	})

	return results, processedFiles
}

// =====================================================================
// Infrastructure / Interfaces (外部依存の抽象化)
// =====================================================================

// ArchiveReader はアーカイブファイルの読み込みを抽象化します。
type ArchiveReader interface {
	ReadEntries(path string) ([]FileEntry, error)
}

// ZipArchiveReader はZIPファイルを実際に読み込む実装です。
type ZipArchiveReader struct{}

func (z ZipArchiveReader) ReadEntries(zipPath string) ([]FileEntry, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open zip: %w", err)
	}
	defer r.Close()

	var entries []FileEntry
	for _, f := range r.File {
		name := f.Name

		// ZIPのフラグを見てUTF-8でない（Shift_JISの可能性が高い）と判定された場合の処理
		if f.NonUTF8 {
			decodedName, err := decodeShiftJIS(name)
			if err == nil {
				name = decodedName // 変換に成功した場合のみ上書き
			}
		}

		entries = append(entries, FileEntry{
			Name:  name,
			IsDir: f.FileInfo().IsDir(),
		})
	}
	return entries, nil
}

// decodeShiftJIS はShift_JISの文字列をUTF-8に変換するヘルパー関数です。(純粋関数)
func decodeShiftJIS(s string) (string, error) {
	decoder := japanese.ShiftJIS.NewDecoder()
	b, _, err := transform.Bytes(decoder, []byte(s))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// WriteCSV は結果をCSV形式でWriterに出力します。
func WriteCSV(w io.Writer, results []FolderCount) error {
	// BOMを出力
	if _, err := w.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return err
	}
	writer := csv.NewWriter(w)
	defer writer.Flush()

	if err := writer.Write([]string{"Folder Path", "File Count"}); err != nil {
		return err
	}
	for _, r := range results {
		if err := writer.Write([]string{r.Path, strconv.Itoa(r.Count)}); err != nil {
			return err
		}
	}
	return nil
}

// WriteText は結果をプレーンテキストでWriterに出力します。
func WriteText(w io.Writer, results []FolderCount) error {
	_, err := fmt.Fprintf(w, "\n%-60s | %s\n", "Folder Path", "File Count")
	if err != nil {
		return err
	}
	fmt.Fprintln(w, strings.Repeat("-", 80))
	for _, r := range results {
		_, err := fmt.Fprintf(w, "%-60s | %d\n", r.Path, r.Count)
		if err != nil {
			return err
		}
	}
	return nil
}

// =====================================================================
// Application (ユースケース)
// =====================================================================

type AppConfig struct {
	ZipPath   string
	Threshold int
	CsvPath   string
}

type App struct {
	Reader ArchiveReader
	Logger *slog.Logger
}

// Run はアプリケーションのメインフローを実行します。
func (app *App) Run(cfg AppConfig, outStream io.Writer) error {
	if cfg.ZipPath == "" {
		return errors.New("zip path is required")
	}

	app.Logger.Info("ZIPファイルの解析を開始します", slog.String("zipPath", cfg.ZipPath))

	entries, err := app.Reader.ReadEntries(cfg.ZipPath)
	if err != nil {
		return fmt.Errorf("read entries error: %w", err)
	}

	results, totalFiles := AggregateFolders(entries, cfg.Threshold)
	app.Logger.Info("集計完了", slog.Int("totalFiles", totalFiles), slog.Int("extractedFolders", len(results)))

	// CSV出力指定がある場合
	if cfg.CsvPath != "" {
		file, err := os.Create(cfg.CsvPath)
		if err != nil {
			return fmt.Errorf("failed to create csv file: %w", err)
		}
		defer file.Close()

		if err := WriteCSV(file, results); err != nil {
			return fmt.Errorf("failed to write csv: %w", err)
		}
		app.Logger.Info("結果をCSVに出力しました", slog.String("csvPath", cfg.CsvPath))
		return nil
	}

	// 画面出力指定の場合
	return WriteText(outStream, results)
}

// =====================================================================
// Entry Point
// =====================================================================

func main() {
	zipPath := flag.String("zip", "", "対象のZIPファイルのパス (必須)")
	threshold := flag.Int("threshold", 10000, "抽出するファイル数のしきい値")
	csvPath := flag.String("csv", "", "結果を出力するCSVファイルのパス (省略時は画面表示)")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	app := &App{
		Reader: ZipArchiveReader{},
		Logger: logger,
	}

	cfg := AppConfig{
		ZipPath:   *zipPath,
		Threshold: *threshold,
		CsvPath:   *csvPath,
	}

	if err := app.Run(cfg, os.Stdout); err != nil {
		logger.Error("アプリケーションエラー", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
