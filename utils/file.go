package utils

import (
	"bytes"
	"io"
	"net/url"
	"path/filepath"

	"izumi/config"

	zaplog "github.com/dokidokikoi/go-common/log/zap"
	"github.com/dokidokikoi/go-common/tools"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

func SaveTmpFile(ext string, data io.Reader) (string, error) {
	return tools.SaveFileWithMd5Name(data, config.TmpDir, ext)
}

// SavedFile 是批量下载结果中的一项。
type SavedFile struct {
	URL  string
	Path string
}

// SaveBunchTmpFiles 并发下载 urls 到临时目录，按输入首次出现顺序返回成功项。
// 失败项只记日志、不返回，也不会产生空文件；重试由 HTTP 层负责。
func SaveBunchTmpFiles(fn func(uri string) ([]byte, error), urls []string, limit ...int) []SavedFile {
	uniq := dedupeURLs(urls)
	if len(uniq) == 0 {
		return nil
	}

	n := config.ImageSaveConcurrency()
	if len(limit) > 0 && limit[0] > 0 {
		n = limit[0]
	}

	type result struct {
		path string
		ok   bool
	}
	results := make([]result, len(uniq))

	group := errgroup.Group{}
	group.SetLimit(n)
	for i, uri := range uniq {
		group.Go(func() error {
			u, err := url.Parse(uri)
			if err != nil {
				zaplog.L().Error("parse url error", zap.String("url", uri), zap.Error(err))
				return nil
			}
			data, err := fn(uri)
			if err != nil {
				zaplog.L().Error("fetch file failed", zap.String("url", uri), zap.Error(err))
				return nil
			}
			if len(data) == 0 {
				zaplog.L().Error("fetch file empty", zap.String("url", uri))
				return nil
			}
			path, err := SaveTmpFile(filepath.Ext(u.Path), bytes.NewBuffer(data))
			if err != nil {
				zaplog.L().Error("save file failed", zap.String("url", uri), zap.Error(err))
				return nil
			}
			results[i] = result{path: path, ok: true}
			return nil
		})
	}
	group.Wait()

	files := make([]SavedFile, 0, len(uniq))
	for i, uri := range uniq {
		if results[i].ok {
			files = append(files, SavedFile{URL: uri, Path: results[i].path})
		}
	}
	return files
}

// SaveBunchTmpFile 是 SaveBunchTmpFiles 的 map 兼容包装。
func SaveBunchTmpFile(fn func(uri string) ([]byte, error), urls []string) map[string]string {
	files := SaveBunchTmpFiles(fn, urls)
	res := make(map[string]string, len(files))
	for _, f := range files {
		res[f.URL] = f.Path
	}
	return res
}

func dedupeURLs(urls []string) []string {
	seen := make(map[string]struct{}, len(urls))
	uniq := make([]string, 0, len(urls))
	for _, uri := range urls {
		if uri == "" {
			continue
		}
		if _, ok := seen[uri]; ok {
			continue
		}
		seen[uri] = struct{}{}
		uniq = append(uniq, uri)
	}
	return uniq
}
