package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"
)

func hashFile(path string) string {
	fp, err := os.Open(path)

	if err != nil {
		Error("hashFile: failed to open file: %s", path)
		return ""
	}

	defer fp.Close()

	fi, err := fp.Stat()

	if err != nil {
		Error("hashFile: failed to stat file: %s", path)
		return ""
	}

	if fi.IsDir() {
		Error("hashFile: is a directory: %s", path)
		return ""
	}

	mac := hmac.New(sha256.New, []byte("infoscreen"))

	_, err = io.Copy(mac, fp)

    if err != nil {
               Error("hashFile: hashing failed: %s", err.Error())
	}

	sum := mac.Sum(nil)

    return base64.RawURLEncoding.EncodeToString(sum)
}

func copyToRepo(path string) string {
	fileHash := hashFile(path)

	if fileHash == "" {
		return ""
	}

	repoFile := fileHash + filepath.Ext(path)

	repoPath := filepath.Join(g_config.RepoRoot, repoFile)

	fp, err := os.Open(repoPath)

	if err == nil {
		defer fp.Close()

		fi, err := fp.Stat()
		if err != nil {
			Error("copyToRepo: failed to stat file: %s: %s", repoPath, err.Error())
			return ""
		}
		if fi.IsDir() {
			Error("copyToRepo: target is a directory: %s: %s", repoPath, err.Error())
			return ""
		}

		return repoFile
	}

	Info(0, "Copying to repo: %s -> %s", path, repoPath)

	fp, err = os.Open(path)

	if err != nil {
		Error("copyToRepo: failed to open file: %s: %s", path, err.Error())
		return ""
	}

	defer fp.Close()


	fpDst, err := os.Create(repoPath)

	if err != nil {
		Error("copyToRepo: failed to create file: %s: %s", repoPath, err.Error())
		return ""
	}

	_, err = io.Copy(fpDst, fp)

	if err != nil {
		Error("copyToRepo: failed to copy file to repo: %s -> %s: %s", path, repoPath, err.Error())
		fpDst.Close()
		return ""
	}

	err = fpDst.Close()

	if err != nil {
		Error("copyToRepo: failed to close file: %s: %s", repoPath, err.Error())
		return ""
	}

	return repoFile
}

func collectFilesFromDirectory(path string, ext FileExtMap) ([]string, time.Time) {
	var res []string
	var ts time.Time

	files, err := ioutil.ReadDir(path)

	if err != nil {
		Error("collectFilesFromDirectory: failed to read directory: %s: %s", path, err.Error())
		return res, ts
	}

	for _, file := range files {
		Info(0, "Checking file: %s", file.Name())
		if file.IsDir() && file.Name() != "." && file.Name() != ".." {
			r, t := collectFilesFromDirectory(filepath.Join(path, file.Name()), ext)
			res = append(res, r...)
			if t.After(ts) {
				ts = t
			}
		} else {
			if ext[filepath.Ext(file.Name())] {
				res = append(res, filepath.Join(path, file.Name()))
				if file.ModTime().After(ts) {
					ts = file.ModTime()
				}
			}
		}
	}

	return res, ts
}

func checkAndImport(sourceDir string, refTimeStamp time.Time, refCount int, ext FileExtMap) ([]string, time.Time) {
	files, ts := collectFilesFromDirectory(sourceDir, ext)

	var res []string

	if !ts.After(refTimeStamp) && len(files) == refCount {
		return nil, ts
	}

	for _, f := range files {
		fh := copyToRepo(f)

		if fh != "" {
			res = append(res, fh)
		}
	}

	return res, ts
}