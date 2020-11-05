package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/nfnt/resize"
	"image"
	_ "image/jpeg"
	"image/png"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Config struct {
	LogFile string
	Verbosity int
	BindPort int
	BindAdr string
	CacheSize uint

	AppRoot string
	RepoRoot string

	ContentSourceDir string
	Content2SourceDir string
	Content3SourceDir string
	ImageSourceDir string
	TickerSourceDir string
	TickerDefaultFile string

	ContentSyncInterval int
	BrowserPath string
	TerminateHour int
	TerminateMinute int

	ScreenConfig int

	ContentImageDisplayDuration int
	MixinImageDisplayDuration int
	MixinImageRate int
	TickerDisplayDuration int
	MaxVideoDuration int

	OpenWeatherMapUrl string
	OpenWeatherMapApiKey string
	OpenWeatherMapCityId string
}

type ImagesResponse struct {
	Serial int32 `json:"serial"`
	ContentImages []Content `json:"content_images"`
	Content2Images []Content `json:"content2_images"`
	Content3Images []Content `json:"content3_images"`
	MixinImages []Content `json:"mixin_images"`
	Ticker []Content `json:"ticker"`
	TickerDefault Content `json:"ticker_default"`
}

type ConfigResponse struct {
	ScreenConfig int `json:"screen_config"`
	OpenWeatherMapUrl string `json:"open_weather_map_url"`
	OpenWeatherMapApiKey string `json:"open_weather_map_api_key"`
	OpenWeatherMapCityId string `json:"open_weather_map_city_id"`

	ContentImageDisplayDuration int `json:"content_image_display_duration"`
	TickerDisplayDuration int `json:"ticker_display_duration"`
	MixinImageDisplayDuration int `json:"mixin_image_display_duration"`
	MixinImageRate int `json:"mixin_image_rate"`
	MaxVideoDuration int `json:"max_video_duration"`

	VideoExtensions []string `json:"video_extensions"`
}


type FileExtMap map[string]bool

var ImageExtensions FileExtMap
var TextExtensions FileExtMap
var VideoExtensions FileExtMap

var Content1 *ContentSource
var Content2 *ContentSource
var Content3 *ContentSource
var Dias *ContentSource
var Ticker *ContentSource
var TickerDefault *ContentSource


var ContentMutex sync.Mutex

var Terminate = false

var HttpServer *http.Server
var BrowserCmd *exec.Cmd




var g_config = Config{LogFile:"infoscreen.log", Verbosity:0, AppRoot:"app", RepoRoot:"rep", BindPort:5000, BindAdr:"localhost",
	ImageSourceDir:"", ContentSourceDir:"",	Content2SourceDir:"",Content3SourceDir:"",
	TickerSourceDir:"tickerSourceDirNotSet", TickerDefaultFile:"TickerDefaultFileNotSet",
	ContentImageDisplayDuration:5, TickerDisplayDuration:5,
	ContentSyncInterval:60, MixinImageDisplayDuration:5, MixinImageRate:2, MaxVideoDuration: 0,
	OpenWeatherMapUrl:"http://api.openweathermap.org/data/2.5",
	ScreenConfig:1, CacheSize:100, TerminateHour:-1 }


func readConfig(filename string) bool {
	data, err := ioutil.ReadFile(filename)

	if err != nil {
		Error("Failed to open config file \"%s\": %s\n", filename, err.Error())
		return false
	}

	err = json.Unmarshal(data, &g_config)
	if err != nil {
		Error("Syntax error in config file %s: %v\n", filename, err)
		return false
	}

	return true
}

func removeUrlRoot(url, root string) string {
	if strings.HasPrefix(url, root) {
		return url[len(root):]
	}

	return url
}

func determineContentType(path string) string {
	switch filepath.Ext(path) {
	case ".html":
		return "text/html"

	case ".css":
		return "text/css"

	case ".js":
		return "application/javascript"

	default:
		return "application/octet-stream"
	}
}


func sendSizedImage(name string, fp *os.File, width, height uint, resp http.ResponseWriter, req *http.Request) {
	cacheImage := GetImageFromCache(name, width, height)

	if cacheImage != nil {
		_, err := io.Copy(resp, bytes.NewReader(cacheImage))
		if err != nil {
			Error("sendSizedImage: failed send cached image: %s", err.Error())
			http.NotFound(resp, req)
		}
		return
	}

	Info(1, "Sizing Image: %s...", fp.Name())

	img, imageType, err := image.Decode(fp)

	if err != nil {
		Error("sendSizedImage: failed to decode image: %s: %s", fp.Name(), err.Error())
		http.NotFound(resp, req)
		return
	}

	Info(1, "image type: %s", imageType)

	w := img.Bounds().Dx()
	h := img.Bounds().Dy()

	Info(1, "Imagesize: %dx%d", w, h)

	size := float32(width)/float32(w)
	h1 := float32(h)*size

	var simg image.Image
	if uint(h1) <= height {
		simg = resize.Resize(width, 0, img, resize.Bicubic)
	}  else {
		simg = resize.Resize(0, height, img, resize.Bicubic)
	}

	encoder := png.Encoder{CompressionLevel:png.BestSpeed}

	imageBuffer := new(bytes.Buffer)

	err = encoder.Encode(imageBuffer, simg)

	if err != nil {
		Error("sendSizedImage: failed to encode image: %s", err.Error())
		http.NotFound(resp, req)
		return
	}

	addImageToCache(name, width, height, imageBuffer.Bytes())

	_, err = io.Copy(resp, imageBuffer)

	if err != nil {
		Error("sendSizedImage: failed send image: %s", err.Error())
	}
}

func serveFile(urlRoot, basePath string, resp http.ResponseWriter, req *http.Request) {
	path := removeUrlRoot(req.URL.Path, urlRoot)

	if path == "" {
		path = "index.html"
	}

	path = filepath.Join(basePath, path)

	Info(1, "Request: %s", req.URL.Path)

	fp, err := os.Open(path)

	if err != nil {
		Error("app request: failed to open file: %s", path)
		http.NotFound(resp, req)
		return
	}

	defer fp.Close()

	resp.Header().Set("Content-Type", determineContentType(path))

	query := req.URL.Query()

	var imgWidth, imgHeight int

	if v := query["w"]; len(v) > 0 {
		imgWidth, _ = strconv.Atoi(v[0])
	}
	if v := query["h"]; len(v) > 0 {
		imgHeight, _ = strconv.Atoi(v[0])
	}

	Info(1, "Found w, h: %d %d", imgWidth, imgHeight)

	if isImageFile(path) && imgWidth > 0 && imgHeight > 0 {
		sendSizedImage(filepath.Base(path), fp, uint(imgWidth), uint(imgHeight), resp, req)
	} else {
		_, err = io.Copy(resp, fp)

		if err != nil {
			Error("app request: failed to copy file to response: %s", path)
			http.NotFound(resp, req)
			return
		}
	}
}

func handleAppRequest(resp http.ResponseWriter, req *http.Request) {
	//resp.Header().Set("Content-Type", "application/json; charset=utf-8")
	//resp.Header().Set("Access-Control-Allow-Origin", "*")

	serveFile("/", g_config.AppRoot, resp, req)
}

func handleRepRequest(resp http.ResponseWriter, req *http.Request) {
	//resp.Header().Set("Content-Type", "application/json; charset=utf-8")
	//resp.Header().Set("Access-Control-Allow-Origin", "*")

	serveFile("/api/rep/", g_config.RepoRoot, resp, req)
}

func buildSerial(content... *ContentSource) int32 {
	var serial int32
	for _, c := range content {
		serial += c.serial
	}
	return serial
}

func handleGetContentRequest(resp http.ResponseWriter, req *http.Request) {
	resp.Header().Set("Content-Type", "application/json; charset=utf-8")
	resp.Header().Set("Access-Control-Allow-Origin", "*")

	ContentMutex.Lock()

	serial := buildSerial(Content1, Content2, Content3, Dias, Ticker, TickerDefault)

	res := ImagesResponse{Serial:serial,
		ContentImages:Content1.content, Content2Images:Content2.content,
		Content3Images:Content3.content, MixinImages:Dias.content,
		Ticker:Ticker.content, TickerDefault:TickerDefault.content[0]}

	d, err := json.Marshal(res)
	if err != nil {
		Error("handleGetImagesRequest: marshal: %v\n", err)
	}

	ContentMutex.Unlock()

	io.WriteString(resp, string(d))
}

func handleGetConfigRequest(resp http.ResponseWriter, req *http.Request) {
	resp.Header().Set("Content-Type", "application/json; charset=utf-8")
	resp.Header().Set("Access-Control-Allow-Origin", "*")

	res := ConfigResponse{ScreenConfig:g_config.ScreenConfig,
		ContentImageDisplayDuration:g_config.ContentImageDisplayDuration,
		TickerDisplayDuration:g_config.TickerDisplayDuration,
		MixinImageRate:g_config.MixinImageRate,
		MaxVideoDuration:g_config.MaxVideoDuration,
		MixinImageDisplayDuration:g_config.MixinImageDisplayDuration,
		OpenWeatherMapUrl:g_config.OpenWeatherMapUrl,
		OpenWeatherMapApiKey:g_config.OpenWeatherMapApiKey,
		OpenWeatherMapCityId:g_config.OpenWeatherMapCityId,
		VideoExtensions:VideoExtensionList}

	d, err := json.Marshal(res)
	if err != nil {
		Error("handleGetConfig: marshal: %v\n", err)
	}
	io.WriteString(resp, string(d))
}

func syncContent() {
	for !Terminate {
		Info(1, "Syncing content...")

		updateContentSources()

		time.Sleep(time.Duration(g_config.ContentSyncInterval) * time.Second)
	}
}

func terminate() {
	time.Sleep(70*time.Second) // avoid direct terminating after re-start

	for {
		now := time.Now()

		if now.Hour() == g_config.TerminateHour && now.Minute() == g_config.TerminateMinute {
			err := HttpServer.Shutdown(context.Background())
			if err != nil {
				Error("Error will stopping http server: %s", err.Error())
			}
			stopBrowser()
			Terminate = true
			return
		}

		time.Sleep(10*time.Second)
	}
}

func startBrowser() {
	time.Sleep(10*time.Second)

	if g_config.BrowserPath != "" {
		url := fmt.Sprintf("http://%s:%d/", "localhost", g_config.BindPort)
		BrowserCmd = exec.Command(g_config.BrowserPath, url)

		if err := BrowserCmd.Start(); err != nil {
			Error("Failed to start browser: %s", err.Error())
			BrowserCmd = nil
		} else {
			BrowserCmd.Wait()
		}
	}
}

func stopBrowser() {
	if BrowserCmd != nil {
		if err := BrowserCmd.Process.Signal(syscall.SIGKILL); err != nil {
			Error("Failed to kill browser: %s", err.Error())
		}
	}
}


func startHttpServer() {
	HttpServer = &http.Server{
		Addr:           fmt.Sprintf("%s:%d", g_config.BindAdr, g_config.BindPort),
		Handler:        nil,
		ReadTimeout:    20 * time.Second,
		WriteTimeout:   20 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	http.HandleFunc("/", handleAppRequest)
	http.HandleFunc("/api/rep/", handleRepRequest)
	http.HandleFunc("/api/content", handleGetContentRequest)
	http.HandleFunc("/api/config", handleGetConfigRequest)

	Info(0,"http server exited: %s", HttpServer.ListenAndServe())
}

func main() {
	if !readConfig("config.json") {
		os.Exit(1)
	}

	gVerbosity = g_config.Verbosity

	if g_config.LogFile != "" {
		if !OpenLogFile(g_config.LogFile) {
			os.Exit(1)
		}
	}

	InitImageCache()

	setupFileExtensions()

	Ticker = getOrCreateContentSource(g_config.TickerSourceDir, ContentSourceTypeTicker)
	TickerDefault = getOrCreateContentSource(g_config.TickerDefaultFile, ContentSourceTypeTickerDefault)
	Content1 = getOrCreateContentSource(g_config.ContentSourceDir, ContentSourceTypeInfo)
	Content2 = getOrCreateContentSource(g_config.Content2SourceDir, ContentSourceTypeInfo)
	Content3 = getOrCreateContentSource(g_config.Content3SourceDir, ContentSourceTypeInfo)
	Dias = getOrCreateContentSource(g_config.ImageSourceDir, ContentSourceTypeDia)

	go syncContent()
	go terminate()
	go startHttpServer()
	go startBrowser()

	for !Terminate {
		time.Sleep(time.Second)
	}

}
