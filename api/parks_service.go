package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/forms"
	"github.com/pocketbase/pocketbase/models"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"github.com/pocketbase/pocketbase/tools/inflector"
	"golang.org/x/image/draw"
)

type Park struct {
	FullName          string   `json:"fullName"`
	Description       string   `json:"description"`
	Latitude          string   `json:"latitude"`
	Longitude         string   `json:"longitude"`
	States            string   `json:"states"`
	Images            []string `json:"-"` // "-" tells the json package to ignore this field when marshaling
	Designation       string   `json:"designation"`
	ParkCode          string   `json:"parkCode"`
	DirectionsInfo    string   `json:"directionsInfo"`
	WeatherInfo       string   `json:"weatherInfo"`
	DriveTime         string
	DrivingDistanceMi string
	DrivingDistanceKm string
	HaversineDistance float64
	ParkRecordId      string
	Weather           []WeatherDate
	Campgrounds       int
	Alerts            []Alert
}

type WeatherDate struct {
	Date              string `json:"date"`
	TemperatureDayF   string `json:"temperatureDayF"`
	TemperatureDayC   string `json:"temperatureDayC"`
	TemperatureNightF string `json:"temperatureNightF"`
	TemperatureNightC string `json:"temperatureNightC"`
	WeatherIcon       string `json:"weatherIcon"`
	LastUpdated       string `json:"lastUpdated"`
}

type Campground struct {
	Id                  string   `json:"id"`
	Name                string   `json:"name"`
	ParkCode            string   `json:"parkCode"`
	Description         string   `json:"description"`
	Latitude            string   `json:"latitude"`
	Longitude           string   `json:"longitude"`
	ReservationInfo     string   `json:"reservationInfo"`
	ReservationURL      string   `json:"reservationUrl"`
	DirectionsOverview  string   `json:"directionsOverview"`
	Images              []string `json:"-"`
	WeatherOverview     string   `json:"weatherOverview"`
	Reservable          string   `json:"numberOfSitesReservable"`
	FirstComeFirstServe string   `json:"numberOfSitesFirstComeFirstServe"`
	MapImage            string   `json:"-"`
}

type Alert struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Category    string `json:"severity"`
	Url         string `json:"url"`
}

var isRunning bool
var mu sync.Mutex

func FetchAndStoreNationalParks(app *pocketbase.PocketBase) error {
	// prevent multiple fetches from running at the same time
	mu.Lock()
	defer mu.Unlock()

	if isRunning {
		log.Printf("National Parks data is already being fetched.")
		return nil
	}
	isRunning = true
	defer func() {
		isRunning = false
	}()
	// fetch data from NPS API
	NPS_API_KEY := os.Getenv("NPS_API_KEY")
	var NPS_API_URL = "https://developer.nps.gov/api/v1/parks?limit=500&api_key=" + NPS_API_KEY
	resp, err := http.Get(NPS_API_URL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// decode the JSON response
	var data struct {
		Data []struct {
			Park
			Images []struct {
				URL string `json:"url"`
			} `json:"images"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return err
	}
	// get the Pocketbase collection for National Parks
	collection, err := app.Dao().FindCollectionByNameOrId("parks")
	if err != nil {
		return err
	}
	// filter for national parks only and store in Pocketbase
	for _, park := range data.Data {
		if park.Designation == "National Park" || park.Designation == "National Park & Preserve" {
			var record *models.Record
			existingRecord, err := app.Dao().FindFirstRecordByData("parks", "parkCode", park.ParkCode)
			if err == nil {
				record = existingRecord
			} else {
				record = models.NewRecord(collection)
				record.Set("parkCode", park.ParkCode)
			}
			// load regular data into the form
			form := forms.NewRecordUpsert(app, record)
			form.LoadData(map[string]any{
				"name":           park.FullName,
				"description":    park.Description,
				"latitude":       park.Latitude,
				"longitude":      park.Longitude,
				"states":         park.States,
				"weatherInfo":    park.WeatherInfo,
				"directionsInfo": park.DirectionsInfo,
			})
			current_images := record.GetStringSlice("images")
		ImageLoop:
			for _, image := range park.Images {
				imageURL := image.URL
				// check if the image is already in the form
				for _, existingImage := range current_images {
					if inflector.Snakecase(quick_strip_url(imageURL)) == quick_strip(existingImage) {
						continue ImageLoop
					}
				}
				// resize the image using my helper function
				resizedImageBytes, err := downloadAndResizeImage(imageURL, 1500)
				if err != nil {
					log.Printf("Error resizing image: %v", err)
					continue
				}
				// save the image to a temporary file
				tmpFile, err := filesystem.NewFileFromBytes(resizedImageBytes, path.Base(imageURL))
				if err != nil {
					log.Printf("Error saving image to a temporary file: %v", err)
					continue
				}
				resizedImageBytes = nil // free up memory
				log.Printf("Adding img to park %s: %f kb", park.ParkCode, float64(tmpFile.Size)/1024.0)
				// add the image to the form
				form.AddFiles("images", tmpFile)
				tmpFile = nil // free up memory
				if err := form.Submit(); err != nil {
					log.Printf("Error saving record with image: %v", err)
					continue
				}
			}
			log.Printf("Park %s has %d images", park.ParkCode, len(record.GetStringSlice("images")))
			campCount, err := fetchCampgrounds(app, record.Id, park.ParkCode)
			if err != nil {
				log.Printf("Error fetching campgrounds: %v", err)
				continue
			}
			form.LoadData(map[string]any{
				"campgrounds": campCount,
			})
			if err := form.Submit(); err != nil {
				log.Printf("Error saving record with image: %v", err)
				continue
			}
		}
	}
	return nil
}

func fetchCampgrounds(app *pocketbase.PocketBase, parkId string, parkCode string) (count int, err error) {
	// fetch data from NPS API
	NPS_API_KEY := os.Getenv("NPS_API_KEY")
	var NPS_API_URL = "https://developer.nps.gov/api/v1/campgrounds?parkCode=" + parkCode + "&api_key=" + NPS_API_KEY
	resp, err := http.Get(NPS_API_URL)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, err
	}
	// decode the JSON response and get image urls from the JSON
	var data struct {
		Data []struct {
			Campground
			Images []struct {
				URL string `json:"url"`
			} `json:"images"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return 0, err
	}
	campgrounds, err := app.Dao().FindCollectionByNameOrId("campgrounds")
	if err != nil {
		return 0, err
	}
	// save the campgrounds to the national park record
	for _, campground := range data.Data {
		var record *models.Record
		// Check if the campground already exists
		existingCampground, err := app.Dao().FindFirstRecordByData("campgrounds", "campId", campground.Id)
		if err == nil {
			record = existingCampground
		} else {
			log.Printf("Creating new record for campground %s", campground.Id)
			record = models.NewRecord(campgrounds)
		}
		form := forms.NewRecordUpsert(app, record)
		reservable, _ := strconv.Atoi(campground.Reservable)
		firstComeFirstServe, _ := strconv.Atoi(campground.FirstComeFirstServe)
		form.LoadData(map[string]any{
			"name":                campground.Name,
			"parkId":              parkId,
			"description":         campground.Description,
			"latitude":            campground.Latitude,
			"longitude":           campground.Longitude,
			"reservationInfo":     campground.ReservationInfo,
			"reservationUrl":      campground.ReservationURL,
			"directionsOverview":  campground.DirectionsOverview,
			"weatherOverview":     campground.WeatherOverview,
			"reservable":          reservable,
			"firstComeFirstServe": firstComeFirstServe,
			"campId":              campground.Id,
		})
		current_images := record.GetStringSlice("images")
	ImageLoop:
		// fetch images for each campground
		for _, image := range campground.Images {
			imageURL := image.URL
			// check if the image is already in the form
			for _, existingImage := range current_images {
				if inflector.Snakecase(quick_strip_url(imageURL)) == quick_strip(existingImage) {
					continue ImageLoop
				}
			}
			// resize the image using my helper function
			resizedImageBytes, err := downloadAndResizeImage(imageURL, 1500)
			if err != nil {
				log.Printf("Error resizing image: %v", err)
				continue
			}
			// save the image to a temporary file
			tmpFile, err := filesystem.NewFileFromBytes(resizedImageBytes, path.Base(imageURL))
			if err != nil {
				log.Printf("Error saving image to a temporary file: %v", err)
				continue
			}
			resizedImageBytes = nil // free up memory
			log.Printf("Resizing campground image: %f kb", float64(tmpFile.Size)/1024.0)
			// add the image to the form
			form.AddFiles("images", tmpFile)
			tmpFile = nil // free up memory
			if err := form.Submit(); err != nil {
				log.Printf("Error saving record with image: %v", err)
				continue
			}
		}
		log.Printf("Camp %s has %d images", record.Id, len(record.GetStringSlice("images")))
		if record.GetString("mapImage") == "" {
			// get map image from mapbox
			firstCome := campground.FirstComeFirstServe != "0"
			imageBytes, err := getMapImage(campground.Latitude, campground.Longitude, firstCome)
			if err != nil {
				log.Printf("Error getting map image: %v", err)
				continue
			}
			tmpfile, err := filesystem.NewFileFromBytes(imageBytes, "map.png")
			if err != nil {
				log.Printf("Error saving map image to a temporary file: %v", err)
				continue
			}
			form.AddFiles("mapImage", tmpfile)
			// save the campground record to the national park record
			if err := form.Submit(); err != nil {
				log.Printf("Error saving record with image: %v", err)
				continue
			}
			imageBytes, tmpfile = nil, nil // free up memory
		}
	}
	return len(data.Data), err
}

func getMapImage(lat, lon string, firstCome bool) ([]byte, error) {
	mapboxAPIKey := os.Getenv("MAPBOX_ACCESS_TOKEN")
	// get map image for the campground (color in url based on whether it's first come first serve)
	var mapImageURL string
	if firstCome {
		mapImageURL = fmt.Sprintf("https://api.mapbox.com/styles/v1/mapbox/outdoors-v12/static/pin-l+65a30d(%s,%s)/%s,%s,15.2,0/768x384@2x?access_token=%s", lon, lat, lon, lat, mapboxAPIKey)
	} else {
		mapImageURL = fmt.Sprintf("https://api.mapbox.com/styles/v1/mapbox/outdoors-v12/static/pin-l+e85151(%s,%s)/%s,%s,15.2,0/768x384@2x?access_token=%s", lon, lat, lon, lat, mapboxAPIKey)
	}
	resp, err := http.Get(mapImageURL)
	if err != nil {
		log.Printf("Error fetching map image: %v", err)
		return nil, err
	}
	defer resp.Body.Close()
	mapImageBytes, _, err := image.Decode(resp.Body)
	if err != nil {
		log.Printf("Error reading map image: %v", err)
		return nil, err
	}
	// save the map image to a temporary file
	byteImage := new(bytes.Buffer)
	err = png.Encode(byteImage, mapImageBytes)
	if err != nil {
		log.Printf("Error encoding map image to a byte slice: %v", err)
		return nil, err
	}
	log.Printf("Saving map image")
	return byteImage.Bytes(), nil
}

// downloadAndResizeImage downloads an image from the given URL and resizes it if it's larger than a maximum width.
func downloadAndResizeImage(url string, maxWidth int) ([]byte, error) {
	// get the image from the NPS API URL
	response, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	// decode the downloaded image
	img, _, err := image.Decode(response.Body)
	if err != nil {
		return nil, err
	}
	// resize the image if it's wider than the maxWidth
	var dst *image.RGBA
	if img.Bounds().Dx() > maxWidth {
		dst = image.NewRGBA(image.Rect(0, 0, maxWidth, img.Bounds().Dy()*maxWidth/img.Bounds().Dx()))
		draw.NearestNeighbor.Scale(dst, dst.Rect, img, img.Bounds(), draw.Over, nil)
	} else {
		dst = image.NewRGBA(image.Rect(0, 0, img.Bounds().Dx(), img.Bounds().Dy()))
		draw.Draw(dst, dst.Bounds(), img, img.Bounds().Min, draw.Over)
	}
	// encode the image back to a byte slice as JPEG
	buf := new(bytes.Buffer)
	err = jpeg.Encode(buf, dst, nil)
	if err != nil {
		return nil, err
	}
	dst, img = nil, nil // free up memory
	return buf.Bytes(), nil
}

// FetchAndStoreWeather fetches weather data for each national park and stores it in the record.
func FetchAndStoreWeather(app *pocketbase.PocketBase) error {
	// get all national parks
	parks, err := app.Dao().FindRecordsByExpr("parks", nil)
	if err != nil {
		return err
	}
	// fetch weather data for each park
	for _, park := range parks {
		lon := park.GetString("longitude")
		lat := park.GetString("latitude")
		apiUrl, err := buildWeatherAPIUrl(lon, lat)
		if err != nil {
			log.Printf("Failed to build weather API URL for park %s: %s", park.GetString("parkCode"), err)
			continue
		}
		weatherData, err := parseWeatherData(apiUrl) // Parse directly from API
		if err != nil {
			log.Printf("Failed to fetch weather for park %s: %s", park.GetString("parkCode"), err)
			continue // Continue with other parks even if one fails
		}
		// save the weather data to the record
		jsonData, err := json.Marshal(weatherData)
		if err != nil {
			log.Printf("Failed to encode weather data for park %s: %s", park.GetString("parkCode"), err)
			return err
		}
		park.Set("weather", jsonData)
		if err := app.Dao().Save(park); err != nil {
			log.Printf("Failed to save weather data for park %s: %s", park.GetString("parkCode"), err)
			return err
		}
		log.Printf("Weather data saved for park %s", park.GetString("parkCode"))
	}
	return nil
}

// ping OpenWeatherMap API
func buildWeatherAPIUrl(lon, lat string) (string, error) {
	OWM_API_KEY := os.Getenv("OWM_API_KEY")
	baseUrl := "https://api.openweathermap.org/data/3.0/onecall"
	params := url.Values{}
	params.Add("lat", lat)
	params.Add("lon", lon)
	params.Add("exclude", "minutely,hourly")
	params.Add("appid", OWM_API_KEY)

	url, err := url.Parse(baseUrl)
	if err != nil {
		return "", err
	}
	url.RawQuery = params.Encode()
	return url.String(), nil
}

func parseWeatherData(apiUrl string) ([]WeatherDate, error) {
	resp, err := http.Get(apiUrl)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var result struct {
		Daily []struct {
			Dt   int64 `json:"dt"`
			Temp struct {
				Day   float64 `json:"day"`
				Night float64 `json:"night"`
			} `json:"temp"`
			Weather []struct {
				Icon string `json:"icon"`
			} `json:"weather"`
		} `json:"daily"`
	}
	err = json.Unmarshal(body, &result)
	if err != nil {
		return nil, err
	}

	var weatherDates []WeatherDate
	for _, daily := range result.Daily {
		date := time.Unix(daily.Dt, 0).Format("Jan 2")
		dayF := kelvinToFahrenheit(daily.Temp.Day)
		dayC := kelvinToCelsius(daily.Temp.Day)
		nightF := kelvinToFahrenheit(daily.Temp.Night)
		nightC := kelvinToCelsius(daily.Temp.Night)
		iconURL := fmt.Sprintf("https://openweathermap.org/img/wn/%s@2x.png", daily.Weather[0].Icon)

		weatherDates = append(weatherDates, WeatherDate{
			Date:              date,
			TemperatureDayF:   dayF,
			TemperatureDayC:   dayC,
			TemperatureNightF: nightF,
			TemperatureNightC: nightC,
			WeatherIcon:       iconURL,
			LastUpdated:       time.Now().Format(time.RFC3339),
		})
	}
	return weatherDates, nil
}

// Kelvin to Fahrenheit
func kelvinToFahrenheit(k float64) string {
	return fmt.Sprintf("%.1f", (k-273.15)*1.8+32)
}

// Kelvin to Celsius
func kelvinToCelsius(k float64) string {
	return fmt.Sprintf("%.1f", k-273.15)
}

func FetchAndStoreWeatherHTTP(app *pocketbase.PocketBase) echo.HandlerFunc {
	log.Printf("=============== FETCHING WEATHER DATA ===============")
	return func(c echo.Context) error {
		err := FetchAndStoreWeather(app)
		if err != nil {
			return c.String(http.StatusInternalServerError, err.Error())
		}
		log.Printf("============= Weather data has been stored successfully. ==============")
		return c.String(http.StatusOK, "Weather data has been stored successfully.")
	}
}

func FetchAndStoreNationalParksHTTP(app *pocketbase.PocketBase) echo.HandlerFunc {
	return func(c echo.Context) error {
		err := FetchAndStoreNationalParks(app)
		if err != nil {
			return c.String(http.StatusInternalServerError, err.Error())
		}
		return c.String(http.StatusOK, "National Parks data has been stored successfully.")
	}
}

// remove file extension and everything to the left of the last /
func quick_strip_url(s string) string {
	filename := path.Base(s)                 // Get the filename
	ext := path.Ext(filename)                // Get the extension
	return strings.TrimSuffix(filename, ext) // Remove the extension from the filename
}

// remove everything from the right up to the first underscore from the right, from a given string
func quick_strip(s string) string {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '_' {
			return s[:i]
		}
	}
	return s
}

// fetch alerts for each national park
func FetchParkAlerts(parkCode string) ([]Alert, error) {
	NPS_API_KEY := os.Getenv("NPS_API_KEY")
	var NPS_API_URL = "https://developer.nps.gov/api/v1/alerts?parkCode=" + parkCode + "&api_key=" + NPS_API_KEY
	resp, err := http.Get(NPS_API_URL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, err
	}
	var data struct {
		Data []struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			Category    string `json:"category"`
			URL         string `json:"url"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	// return Alert struct
	var alerts []Alert
	for _, alert := range data.Data {
		alerts = append(alerts, Alert{
			Title:       alert.Title,
			Description: alert.Description,
			Category:    alert.Category,
			Url:         alert.URL,
		})
	}
	return alerts, nil
}

func FetchAlerts(app *pocketbase.PocketBase) error {
	// remove all alerts records from collection
	collection, err := app.Dao().FindCollectionByNameOrId("alerts")
	if err != nil {
		return err
	}
	alerts, err := app.Dao().FindRecordsByExpr(collection.Name, nil)
	if err != nil {
		return err
	}
	for _, alert := range alerts {
		if err := app.Dao().Delete(alert); err != nil {
			return err
		}
	}
	// get all national parks
	parks, err := app.Dao().FindRecordsByExpr("parks", nil)
	if err != nil {
		return err
	}
	// fetch alerts for each park
	for _, park := range parks {
		parkCode := park.GetString("parkCode")
		alerts, err := FetchParkAlerts(parkCode)
		if err != nil {
			log.Printf("Failed to fetch alerts for park %s: %s", parkCode, err)
			continue
		}
		// save the alerts to the record
		for _, alert := range alerts {
			record := models.NewRecord(collection)
			form := forms.NewRecordUpsert(app, record)
			form.LoadData(map[string]any{
				"title":       alert.Title,
				"description": alert.Description,
				"category":    alert.Category,
				"url":         alert.Url,
				"park":        park.Id,
			})
			log.Printf("Saving alert for park %s", parkCode)
			if err := form.Submit(); err != nil {
				log.Printf("Failed to save alert for park %s: %s", parkCode, err)
				continue
			}
		}
	}
	return nil
}

func FetchAlertsHTTP(app *pocketbase.PocketBase) echo.HandlerFunc {
	return func(c echo.Context) error {
		log.Printf("=============== FETCHING ALERTS DATA ===============")
		err := FetchAlerts(app)
		if err != nil {
			return c.String(http.StatusInternalServerError, err.Error())
		}
		log.Printf("============= Alerts data has been stored successfully. ==============")
		return c.String(http.StatusOK, "Alerts data has been stored successfully.")
	}
}
