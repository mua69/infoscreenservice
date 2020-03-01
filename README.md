# Infoscreen - infoscreenservice

## Introduction

This is a http server including a simple content management system to be used in
combination with the Angular based application [infoscreenapp](https://github.com/mua69/infoscreenapp) providing
an infoscreen solution for a school. The system is intended to run on a Mini/Stick PC or a Rasperry PI connected
to a standard TV or monitor.

The content management system offers following:
* 3 independent content image sequences (content-1, content-2, content-3)
* 1 dia show image sequence
* 1 ticker message sequence
* 1 ticker default message

All content is represented as image files (JPG or PNG, ideally with a 16:9 aspect ratio, but any aspect ratio is fine)
or as text files for the ticker, which are taken from content source directories. The content source directories are usually
located on a mounted network drive. Content is simply supplied by adding, editing or removing files in the source directories. 
 
The content source directories and their sub-directories are regularly scanned for updates. New content files are transferred
to a local repository directory using the SHA256 hash value over the file content for building unique file names. 

The content lists are built by recursively collecting all supported files from the respective source directory.
The order of the lists is determined by alphabetically sorting the source directory content so that the order of
the displayed content images can be controlled via the file names.

Ticker text files may contain multiple ticker messages, which must be separated by an empty line. 
Text files must use UTF-8 character encoding. If an invalid UTF-8 character encoding is detected, 
an conversion from Windows code page 1252 to UTF-8 is implicitly performed so that text files created
with older versions of the Windows text editor are correctly handled.
 
Optionally, a web browser is started pointing to the served `/` endpoint (see `BrowserPath` configuration). 

Optionally, the server terminates itself at a given time point, see `TerminateHour` and `TerminateMinute` configuration.

## Build and Execute

1. Install and setup latest Go release from https://golang.org/ .
1. Get and compile this package: `go get github.com/mua69/infoscreenservice`.
1. Build infoscreenapp as described in https://github.com/mua69/infoscreenapp.
1. Create repository and source directories.
1. Provide user configuration in `config.json`. A sample config file is located in the package directory `$GOPATH/src/github.com/mua69/infoscreenservice`.
1. Run `$GOPATH/bin/infoscreenservice`

## User Configuration

The user configuration is read from file `./config.json` in JSON syntax once upon startup.

Key | Type | Description
--- | ---- | -----------
LogFile | string | Location of log file. Empty string disables logging.
BindPort | int | Listening port of HTTP server. Defaults to `5000`.
BindAdr | string | Listing IP of HTTP server. Default to `localhost`
CacheSize | uint | Size of image cache in MB. Defaults to `100`.
AppRoot | string | Location of infoscreenapp. Directory hierarchy is served through endpoint `/`.
RepoRoot | string | Location of content repository. Defaults to `rep`.
ContentSourceDir | string | Directory containing content-1 images.
Content2SourceDir | string | Directory containing content-2 images.
Content3SourceDir | string | Directory containing content-3 images.
ImageSourceDir | string | Directory containing dia show images.
TickerSourceDir | string | Directory containing ticker text files.
TickerDefaultFile | string | Path to text file containing ticker default message.
ContentSyncInterval | int | Interval in seconds in which content, dia show and ticker directories are scanned for updates. Defaults to `60`.
BrowserPath | string | Path to a web browser executable. Use empty string to disable.
TerminateHour | int | Hour at which this service exits. Use a negative value to disable. Defaults to `-1`.
TerminateMinute | int | Minute at which this service exits.
ScreenConfig | int | Value can be 1 or 4. Screen configuration: 1: - single content image with intermixed dia show images , 4: 3 content images and 1 dia show image in 2x2 arrangement. Defaults to 1.
ContentImageDisplayDuration | int | Display duration in seconds for content images. Defaults to 5.
MixinImageDisplayDuration | int | Display duration in seconds for dia show images. Defaults to 5.
MixinImageRate | int | Only used for `ScreenConfig`:`1`. Mixin rate of dia show images with content images. E.g. the default value 2 means that every 2 content images 1 dia show image is displayed.
TickerDisplayDuration | int | Display duration in seconds for ticker messages.
OpenWeatherMapUrl | string | URL to OpenWeather API, defaults to `http://api.openweathermap.org/data/2.5`.
OpenWeatherMapApiKey | string | Key for accessing OpenWeather API. Can be obtained for free from https://openweathermap.org/ .
OpenWeatherMapCityId | string | ID of city/location for which the weather data shall be displayed. The city ID can be obtained from https://openweathermap.org/current#cityid .


## HTTP Endpoints

List of endpoints provided by the http server:

Endpoint | Description
-------- | -----------
/ | Serves the content of the directory configured with `AppRoot`. This is usually the compiled `infoscreenapp`.
/api/rep | Serves image or text files from the content repository (location configured with `RepoRoot`). Images files can be resized using the URL queries `w` and `h` specifying the desired image width and height in pixels. 
/api/config | Returns JSON object with configuration data for the Angular application.
/api/content | Returns JSON object with content lists.

### Application Config JSON

Configuration JSON data for [infoscreenapp](https://github.com/mua69/infoscreenapp) Angular application provided through endpoint `/api/config`. The data is derived from the user configuration.

Key | Type | Corresponding User Configuration
--- | ---- | --------------------------------
ScreenConfig | int | `ScreenConfig`
OpenWeatherMapUrl | string | `OpenWeatherMapUrl`
OpenWeatherMapApiKey | string | `OpenWeatherMapApiKey`
OpenWeatherMapCityId | string | `OpenWeatherMapCityId`
TickerDisplayDuration | int | `TickerDisplayDuration`
MixinImageDisplayDuration | int |  `MixinImageDisplayDuration`
MixinImageRate | int | `MixinImageRate`

### Content JSON

Content JSON data for [infoscreenapp](https://github.com/mua69/infoscreenapp) Angular application provided through endpoint `/api/content`. 
The content lists are build by scanning content directories specified in the user configuration. 

Key | Type | Description
--- | ---- | -----------
ContentImages | string list | List of content images content-1.
Content2Images | string list | List of content images content-2.
Content3Images | string list | List of content images content-3.
MixinImages | string list | List of dia show images.
Ticker | string list | List of ticker messages.
TickerDefault | string | Default ticker message, which is used when `Ticker` list is empty.

