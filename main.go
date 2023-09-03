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
	LogFile   string
	Verbosity int

	ContentSyncInterval int
	BrowserPath         string
	TerminateHour       int
	TerminateMinute     int

	CacheSize uint
	RepoRoot string

	OpenWeatherMapUrl    string
	OpenWeatherMapApiKey string
	OpenWeatherMapCityId string

	Servers []ServerConfig
}

type ServerConfig struct {
	BindPort int
	BindAdr string

	AppRoot string

	ContentSourceDir string
	Content2SourceDir string
	Content3SourceDir string
	ImageSourceDir string
	TickerSourceDir string
	TickerDefaultFile string

	ScreenConfig uint
	ContentImageDisplayDuration uint
	MixinImageDisplayDuration uint
	MixinImageRate uint
	TickerDisplayDuration uint
	MaxVideoDuration uint
}

type ContentResponse struct {
	Serial int32 `json:"serial"`
	ContentImages []Content `json:"content_images"`
	Content2Images []Content `json:"content2_images"`
	Content3Images []Content `json:"content3_images"`
	MixinImages []Content `json:"mixin_images"`
	Ticker []Content `json:"ticker"`
	TickerDefault Content `json:"ticker_default"`
}

type ConfigResponse struct {
	ScreenConfig uint `json:"screen_config"`
	OpenWeatherMapUrl string `json:"open_weather_map_url"`
	OpenWeatherMapApiKey string `json:"open_weather_map_api_key"`
	OpenWeatherMapCityId string `json:"open_weather_map_city_id"`

	ContentImageDisplayDuration uint `json:"content_image_display_duration"`
	TickerDisplayDuration uint `json:"ticker_display_duration"`
	MixinImageDisplayDuration uint `json:"mixin_image_display_duration"`
	MixinImageRate uint `json:"mixin_image_rate"`
	MaxVideoDuration uint `json:"max_video_duration"`

	VideoExtensions []string `json:"video_extensions"`
}


type FileExtMap map[string]bool

var ImageExtensions FileExtMap
var TextExtensions FileExtMap
var VideoExtensions FileExtMap

type Server struct {
	index int
	config ServerConfig
	httpServer *http.Server
	content1 *ContentSource
	content2 *ContentSource
	content3 *ContentSource
	dias *ContentSource
	ticker *ContentSource
	tickerDefault *ContentSource
}

var Servers []*Server

var ContentMutex sync.Mutex

var Terminate = false

var BrowserCmd *exec.Cmd




var g_config = Config{LogFile:"infoscreen.log",
	Verbosity:0,
	ContentSyncInterval:60,
	OpenWeatherMapUrl:"http://api.openweathermap.org/data/2.5",
	CacheSize:100,
	RepoRoot: "rep",
	TerminateHour:-1 }


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

func handleAppRequest(server *Server, resp http.ResponseWriter, req *http.Request) {
	//resp.Header().Set("Content-Type", "application/json; charset=utf-8")
	//resp.Header().Set("Access-Control-Allow-Origin", "*")

	serveFile("/", server.config.AppRoot, resp, req)
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

func handleGetContentRequest(server *Server, resp http.ResponseWriter, req *http.Request) {
	resp.Header().Set("Content-Type", "application/json; charset=utf-8")
	resp.Header().Set("Access-Control-Allow-Origin", "*")

	ContentMutex.Lock()

	serial := buildSerial(server.content1, server.content2, server.content3,
		server.dias, server.ticker, server.tickerDefault)

	res := ContentResponse{Serial:serial,
		ContentImages:server.content1.content, Content2Images:server.content2.content,
		Content3Images:server.content3.content, MixinImages:server.dias.content,
		Ticker:server.ticker.content, TickerDefault:server.tickerDefault.content[0]}

	d, err := json.Marshal(res)
	if err != nil {
		Error("handleGetImagesRequest: marshal: %v\n", err)
	}

	ContentMutex.Unlock()

	io.WriteString(resp, string(d))
}

func handleGetConfigRequest(server *Server, resp http.ResponseWriter, req *http.Request) {
	resp.Header().Set("Content-Type", "application/json; charset=utf-8")
	resp.Header().Set("Access-Control-Allow-Origin", "*")

	res := ConfigResponse{ScreenConfig:server.config.ScreenConfig,
		ContentImageDisplayDuration:server.config.ContentImageDisplayDuration,
		TickerDisplayDuration:server.config.TickerDisplayDuration,
		MixinImageRate:server.config.MixinImageRate,
		MaxVideoDuration:server.config.MaxVideoDuration,
		MixinImageDisplayDuration:server.config.MixinImageDisplayDuration,
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
			for _, server := range Servers {
				err := server.httpServer.Shutdown(context.Background())
				if err != nil {
					Error("Error will stopping http server[%d]: %s", server.index, err.Error())
				}
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
		url := fmt.Sprintf("http://%s:%d/", "localhost", Servers[0].config.BindPort)

		for {
			Info(0, "Starting browser: %s", g_config.BrowserPath)

			BrowserCmd = exec.Command(g_config.BrowserPath, url)

			if err := BrowserCmd.Start(); err != nil {
				Error("Failed to start browser: %s", err.Error())
				BrowserCmd = nil
			} else {
				err = BrowserCmd.Wait()
				if err != nil {
					Info(0, "Browser exited with error: %s", err.Error())
				} else {
					Info(0, "Browser exited.")
				}
			}
			time.Sleep(time.Second)
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


func startHttpServer(server *Server) {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(resp http.ResponseWriter, req *http.Request){handleAppRequest(server, resp, req)})
	mux.HandleFunc("/api/rep/", handleRepRequest)
	mux.HandleFunc("/api/content", func(resp http.ResponseWriter, req *http.Request){handleGetContentRequest(server, resp, req)})
	mux.HandleFunc("/api/config", func(resp http.ResponseWriter, req *http.Request){handleGetConfigRequest(server, resp, req)})

	server.httpServer = &http.Server{
		Addr:           fmt.Sprintf("%s:%d", server.config.BindAdr, server.config.BindPort),
		Handler:        mux,
		ReadTimeout:    20 * time.Second,
		WriteTimeout:   20 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	Info(0,"http server [%d] exited: %s", server.index, server.httpServer.ListenAndServe())
}

func checkServerConfig(server *Server) {
	err := false
	c := &server.config
	serverIndex := server.index

	if c.BindAdr == "" {
		c.BindAdr = "localhost"
	}

	if c.BindPort == 0 {
		Error("Server[%d]: bind port not set.", serverIndex)
		err = true
	}

	if c.BindPort < 0 {
		Error("Server[%d]: invalid bind port: %d", serverIndex, c.BindPort)
		err = true
	}

	if c.ContentImageDisplayDuration == 0 {
		c.ContentImageDisplayDuration = 5
	}

	if c.TickerDisplayDuration == 0 {
		c.TickerDisplayDuration = 5
	}

	if c.MixinImageDisplayDuration == 0 {
		c.MixinImageDisplayDuration = 5
	}

	if c.MixinImageRate == 0 {
		c.MixinImageRate = 2
	}

	if c.ScreenConfig == 0 {
		c.ScreenConfig = 1
	} else if c.ScreenConfig != 1 && c.ScreenConfig != 4 {
		Error("Server[%d]: invalid ScreenConfig: %d", serverIndex, c.ScreenConfig)
		err = true
	}

	if c.AppRoot == "" {
		Error("Server[%d]: AppRoot not set.", serverIndex)
		err = true
	}

	if err {
		Fatal("Server[%d]: Error(s) in server configuration", serverIndex)
	}
}

func setupServers() {
	if len(g_config.Servers) == 0 {
		Fatal("No servers configured.")
	}

	Servers = make([]*Server, len(g_config.Servers))
	for i := range g_config.Servers {
		server := new(Server)
		Servers[i] = server
		server.index = i
		server.config = g_config.Servers[i]
		checkServerConfig(server)
		server.ticker = getOrCreateContentSource(server.config.TickerSourceDir, ContentSourceTypeTicker)
		server.tickerDefault = getOrCreateContentSource(server.config.TickerDefaultFile, ContentSourceTypeTickerDefault)
		server.content1 = getOrCreateContentSource(server.config.ContentSourceDir, ContentSourceTypeInfo)
		server.content2 = getOrCreateContentSource(server.config.Content2SourceDir, ContentSourceTypeInfo)
		server.content3 = getOrCreateContentSource(server.config.Content3SourceDir, ContentSourceTypeInfo)
		server.dias = getOrCreateContentSource(server.config.ImageSourceDir, ContentSourceTypeDia)
		go startHttpServer(server)
	}
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



	go syncContent()
	go terminate()
	setupServers()
	go startBrowser()

	for !Terminate {
		time.Sleep(time.Second)
	}

}
