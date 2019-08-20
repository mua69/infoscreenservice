package main

import (
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
	BindPort int
	BindAdr string
	AppRoot string
	RepoRoot string
	ContentSourceDir string
	ImageSourceDir string
	TickerSourceDir string
	ContentSyncInterval int
	BrowserPath string
	TerminateHour int
	TerminateMinute int
	ContentImageDisplayDuration int
	MixinImageDisplayDuration int
	MixinImageRate int
	TickerDisplayDuration int

	OpenWeatherMapUrl string
	OpenWeatherMapApiKey string
	OpenWeatherMapCityId string
}

type ImagesResponse struct {
	ContentImages []string `json:"content_images"`
	MixinImages []string `json:"mixin_images"`
	Ticker []string `json:"ticker"`
}

type ConfigResponse struct {
	OpenWeatherMapUrl string `json:"open_weather_map_url"`
	OpenWeatherMapApiKey string `json:"open_weather_map_api_key"`
	OpenWeatherMapCityId string `json:"open_weather_map_city_id"`

	ContentImageDisplayDuration int `json:"content_image_display_duration"`
	TickerDisplayDuration int `json:"ticker_display_duration"`
	MixinImageDisplayDuration int `json:"mixin_image_display_duration"`
	MixinImageRate int `json:"mixin_image_rate"`
}


type FileExtMap map[string]bool

var ImageExtensions FileExtMap
var TextExtensions FileExtMap

var ContentList []string
var ContentTimeStamp time.Time

var DiaShowList []string
var DiaShowTimeStamp time.Time

var Ticker []string
var TickerList []string
var TickerTimeStamp time.Time

var ContentMutex sync.Mutex

var Terminate = false

var HttpServer *http.Server
var BrowserCmd *exec.Cmd



var g_config = Config{LogFile:"infoscreen.log", AppRoot:"app", RepoRoot:"rep", BindPort:5000, BindAdr:"localhost",
	ImageSourceDir:"imageSourceDirNotSet", ContentSourceDir:"contentSourceDirNotSet",
	TickerSourceDir:"tickerSourceDirNotSet",
	ContentImageDisplayDuration:5, TickerDisplayDuration:5,
	ContentSyncInterval:60, MixinImageDisplayDuration:5, MixinImageRate:2,
	OpenWeatherMapUrl:"http://api.openweathermap.org/data/2.5"}


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

func setupFileExtensions() {
	ImageExtensions = make(FileExtMap)
	TextExtensions = make(FileExtMap)

	ImageExtensions[".jpg"] = true
	ImageExtensions[".png"] = true

	TextExtensions[".txt"] = true

}


func parserTickerFile(path string) []string {
	buf, err := ioutil.ReadFile(path)

	var res []string


	if err != nil {
		Error("Failed to read ticker file: %s: %s", path, err.Error())
		return nil
	}

	tickerData := strings.Split(string(buf), "\n")

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

func isImageFile(filename string) bool {
	return ImageExtensions[strings.ToLower(filepath.Ext(filename))]
}

func isTextFile(filename string) bool {
	return TextExtensions[strings.ToLower(filepath.Ext(filename))]
}

func sendSizedImage(fp *os.File, width, height uint, resp http.ResponseWriter, req *http.Request) {
	Info(1, "Sizing Image: %s...", fp.Name())

	img, imageType, err := image.Decode(fp)

	if err != nil {
		Error("sendSizedImage: failed to decode image: %s: s", fp.Name(), err.Error)
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

	err = png.Encode(resp, simg)

	if err != nil {
		Error("sendSizedImage: failed to encode image: %s", err.Error())
		http.NotFound(resp, req)
		return
	}
}

func serveFile(urlRoot, basePath string, resp http.ResponseWriter, req *http.Request) {
	path := removeUrlRoot(req.URL.Path, urlRoot)

	if path == "" {
		path = "index.html"
	}

	path = filepath.Join(basePath, path)

	Info(0, "Request: %s", req.URL.Path)

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
		sendSizedImage(fp, uint(imgWidth), uint(imgHeight), resp, req)
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

func handleGetContentRequest(resp http.ResponseWriter, req *http.Request) {
	resp.Header().Set("Content-Type", "application/json; charset=utf-8")
	resp.Header().Set("Access-Control-Allow-Origin", "*")

	ContentMutex.Lock()

	res := ImagesResponse{ContentImages:ContentList, MixinImages:DiaShowList,
		Ticker:Ticker}

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

	res := ConfigResponse{ContentImageDisplayDuration:g_config.ContentImageDisplayDuration,
		TickerDisplayDuration:g_config.TickerDisplayDuration,
		MixinImageRate:g_config.MixinImageRate,
		MixinImageDisplayDuration:g_config.MixinImageDisplayDuration,
		OpenWeatherMapUrl:g_config.OpenWeatherMapUrl,
		OpenWeatherMapApiKey:g_config.OpenWeatherMapApiKey,
		OpenWeatherMapCityId:g_config.OpenWeatherMapCityId}

	d, err := json.Marshal(res)
	if err != nil {
		Error("handleGetConfig: marshal: %v\n", err)
	}
	io.WriteString(resp, string(d))
}

func syncContent() {
	for !Terminate {
		Info(1, "Syncing content...")

		refCnt := len(ContentList)

		nl, ts := checkAndImport(g_config.ContentSourceDir, ContentTimeStamp, refCnt, isImageFile)

		if nl != nil {
			ContentMutex.Lock()

			ContentList = nl
			ContentTimeStamp = ts

			ContentMutex.Unlock()

			Info(0, "New Content List")
			for _, i := range ContentList {
				Info(0, "  %s", i)
			}
		}

		refCnt = len(DiaShowList)
		nl, ts = checkAndImport(g_config.ImageSourceDir, DiaShowTimeStamp, refCnt, isImageFile)
		if nl != nil {
			ContentMutex.Lock()

			DiaShowList = nl
			DiaShowTimeStamp = ts

			ContentMutex.Unlock()

			Info(0, "New DiaShow List")
			for _, i := range DiaShowList {
				Info(0, "  %s", i)
			}
		}

		refCnt = len(TickerList)
		nl, ts = checkAndImport(g_config.TickerSourceDir, TickerTimeStamp, refCnt, isTextFile)
		if nl != nil {
			ContentMutex.Lock()

			TickerList = nl
			TickerTimeStamp = ts
			Ticker = nil

			Info(0, "New Ticker List")
			for _, i := range TickerList {
				Info(0, "  %s", i)

				tent := parserTickerFile(filepath.Join(g_config.RepoRoot, i))
				if tent != nil {
					Ticker = append(Ticker, tent...)
				}
			}

			ContentMutex.Unlock()
		}


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
	time.Sleep(5*time.Second)

	if g_config.BrowserPath != "" {
		url := fmt.Sprintf("http://%s:%d/", "localhost", g_config.BindPort)
		BrowserCmd = exec.Command(g_config.BrowserPath, url)

		if err := BrowserCmd.Start(); err != nil {
			Error("Failed to start browser: %s", err.Error())
			BrowserCmd = nil
		}
	}
}

func stopBrowser() {
	if BrowserCmd != nil {
		if err := BrowserCmd.Process.Signal(syscall.SIGKILL); err != nil {
			Error("Failed to kill browser: %s", err.Error())
		}

		BrowserCmd.Wait()
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

	if g_config.LogFile != "" {
		if !OpenLogFile(g_config.LogFile) {
			os.Exit(1)
		}
	}


	setupFileExtensions()

	Ticker = make([]string, 0, 10)
	ContentList = make([]string, 0, 10)
	DiaShowList = make([]string, 0, 10)

	go syncContent()
	go terminate()
	go startHttpServer()
	go startBrowser()

	for !Terminate {
		time.Sleep(time.Second)
	}

}
