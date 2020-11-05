package main

import (
	"fmt"
	"golang.org/x/text/encoding/charmap"
	"io/ioutil"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

type ContentSourceType int

type Content struct {
	Type string `json:"type"`
	RepoUrl string `json:"repo_url"`
	Text string `json:"text"`
}

type ContentSource struct {
	contentType ContentSourceType
	sourcePath string
	sourceHash string
	selectFunc func(string) bool
	content []Content
	serial int32
}


const ContentTypeImage = "i"
const ContentTypeVideo = "v"
const ContentTypeText = "t"


const ContentSourceTypeInfo = ContentSourceType(1)
const ContentSourceTypeDia = ContentSourceType(2)
const ContentSourceTypeTicker = ContentSourceType(3)
const ContentSourceTypeTickerDefault = ContentSourceType(4)

var ContentSources = make(map[string]*ContentSource)

var VideoExtensionList = []string{".mp4", ".mov"}
var ImageExtensionList = []string{".jpg", ".png"}
var TextExtensionList = []string{".txt"}


func setupFileExtensions() {
	ImageExtensions = make(FileExtMap)
	TextExtensions = make(FileExtMap)
	VideoExtensions = make(FileExtMap)

	for _, e := range ImageExtensionList {
		ImageExtensions[e] = true
	}

	for _, e := range TextExtensionList {
		TextExtensions[e] = true
	}
	
	for _, e := range VideoExtensionList {
		VideoExtensions[e] = true
	}
}

func isImageFile(filename string) bool {
	return ImageExtensions[strings.ToLower(filepath.Ext(filename))]
}

func isTextFile(filename string) bool {
	return TextExtensions[strings.ToLower(filepath.Ext(filename))]
}

func isVideoFile(filename string) bool {
	return VideoExtensions[strings.ToLower(filepath.Ext(filename))]
}

func isImageOrVideoFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))

	return ImageExtensions[ext] || VideoExtensions[ext]
}


func getOrCreateContentSource(path string, contentType ContentSourceType) *ContentSource {
	key := fmt.Sprintf("%s_%d", path, contentType)

	src := ContentSources[key]
	if src != nil {
		return src
	}

	src = &ContentSource{contentType:contentType, sourcePath: path, serial:0}
    src.content = make([]Content, 0)

    switch contentType {
	case ContentSourceTypeInfo:
		src.selectFunc = isImageOrVideoFile

	case ContentSourceTypeDia:
		src.selectFunc = isImageOrVideoFile

	case ContentSourceTypeTicker:
		src.selectFunc = isTextFile

	case ContentSourceTypeTickerDefault:
		src.content = make([]Content, 1)
		src.content[0].Type = ContentTypeText
	}

	ContentSources[key] = src

	return src
}

func updateContentSources() {
	for _, src := range ContentSources {
		if src.sourcePath != "" {
			if src.contentType == ContentSourceTypeTickerDefault {
				hash := hashFile(src.sourcePath)
				if hash != "" && hash != src.sourceHash {
					ContentMutex.Lock()

					tl := parserTickerFile(src.sourcePath)
					src.content = make([]Content, 1)
					src.content[0].Type = ContentTypeText
					if len(tl) > 0 {
						src.content[0].Text = tl[0]
					}
					src.sourceHash = hash
					src.serial += 1
					Info(0, "New Ticker Default: \"%s\"", src.content[0].Text)

					ContentMutex.Unlock()
				}
			} else {
				nl, h := checkAndImport(src.sourcePath, src.sourceHash, src.selectFunc)

				if nl != nil {
					Info(0, "New content list from source: %s", src.sourcePath)

					ContentMutex.Lock()

					src.sourceHash = h
					src.serial += 1

					src.content = make([]Content, 0, 10)

					for _, i := range nl {
						Info(0, "  %s", i)

						switch src.contentType {
						case ContentSourceTypeTicker:
							tent := parserTickerFile(filepath.Join(g_config.RepoRoot, i))
							for _, s := range tent {
								src.content = append(src.content, Content{Type: ContentTypeText, Text: s})
							}

						case ContentSourceTypeInfo, ContentSourceTypeDia:
							if isVideoFile(i) {
								src.content = append(src.content, Content{Type: ContentTypeVideo, RepoUrl: i})
							} else {
								src.content = append(src.content, Content{Type: ContentTypeImage, RepoUrl: i})
							}
						}
					}

					ContentMutex.Unlock()
				}
			}
		}
	}
}

func parserTickerFile(path string) []string {
	buf, err := ioutil.ReadFile(path)

	var res []string


	if err != nil {
		Error("Failed to read ticker file: %s: %s", path, err.Error())
		return nil
	}

	sbuf := string(buf)

	if !utf8.Valid(buf) {
		Info(1, "Converting to UTF8")
		s, err := charmap.Windows1252.NewDecoder().String(string(buf))
		if err == nil {
			sbuf = s
		} else {
			Error("Decoding windows-1252 to UTF8 failed: %s", err.Error())
		}
	}

	tickerData := strings.Split(sbuf, "\n")

	state := 0 // states: 0: remove empty lines, 1: collect ticker data
	tickerEnt := ""
	for _, s := range tickerData {
		s = strings.TrimSpace(s)

		if s == "" {
			if state == 0 {
				// eat up emtpy line
				continue
			}
			// state is 1, end of ticker entry
			res = append(res, tickerEnt)
			tickerEnt = ""
			state = 0
		} else {
			state = 1
			if tickerEnt != "" {
				tickerEnt += " "
			}
			tickerEnt += s
		}
	}

	if state == 1 {
		res = append(res, tickerEnt)
	}

	return res
}
