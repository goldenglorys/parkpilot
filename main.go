package main

import (
	"log"
	"net/http"
	"os"
	"parkpilot/api"
	"parkpilot/components"
	"parkpilot/template"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
	"github.com/labstack/echo/v5"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/cron"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	app := pocketbase.New()

	// Read the environment variable
	mapboxAccessToken := os.Getenv("MAPBOX_ACCESS_TOKEN")
	npsApiKey := os.Getenv("NPS_API_KEY")

	if mapboxAccessToken == "" {
		log.Fatal("MAPBOX_ACCESS_TOKEN environment variable is not set")
	}
	if npsApiKey == "" {
		log.Fatal("NPS_API_KEY environment variable is not set")
	}

	// serves static files from the provided public dir (if exists)
	app.OnBeforeServe().Add(func(e *core.ServeEvent) error {
		template.NewTemplateRenderer(e.Router)

		e.Router.GET("/*", apis.StaticDirectoryHandler(os.DirFS("./pb_public"), false))

		e.Router.GET("/", func(c echo.Context) error {
			return template.Html(c, components.Index(mapboxAccessToken))
		})

		e.Router.POST("/fetch-parks", func(c echo.Context) error {
			longitude, err := strconv.ParseFloat(c.FormValue("longitude"), 64)
			if err != nil {
				return c.String(http.StatusBadRequest, "Invalid longitude value")
			}
			latitude, err := strconv.ParseFloat(c.FormValue("latitude"), 64)
			if err != nil {
				return c.String(http.StatusBadRequest, "Invalid latitude value")
			}
			placeName := c.FormValue("placeName")
			placeName = strings.Split(placeName, ",")[0] + ", " + strings.Split(placeName, ",")[1]

			// get all records from parks collection
			records, err := app.Dao().FindRecordsByExpr("parks", nil)
			if err != nil {
				return c.String(http.StatusInternalServerError, err.Error())
			}
			// Convert records to Park structs
			var parks []api.Park
			for _, record := range records {
				var park api.Park
				park.FullName = record.GetString("name")
				park.Description = record.GetString("description")
				park.States = record.GetString("states")
				park.Images = record.Get("images").([]string)
				park.Longitude = record.GetString("longitude")
				park.Latitude = record.GetString("latitude")
				park.ParkRecordId = record.Id
				parks = append(parks, park)
			}
			// Fetch driving distances
			parks, err = api.FetchDrivingDistances([2]float64{latitude, longitude}, parks)
			if err != nil {
				return c.String(http.StatusInternalServerError, err.Error())
			}
			collection, err := app.Dao().FindCollectionByNameOrId("nationalParks")
			if err != nil {
				return c.String(http.StatusInternalServerError, err.Error())
			}
			collection_id := collection.Id
			return template.Html(c, components.Parks(parks, placeName, collection_id))
		})

		// route to fetch parks, commented because Pocketbase scheduler is set up to fetch parks every week
		e.Router.GET("/fetchParks", api.FetchAndStoreNationalParks(app))

		// Start a cron that fetches and stores National Parks data once a week
		scheduler := cron.New()
		scheduler.MustAdd("updateParks", "0 0 * * 0", func() {
			api.FetchAndStoreNationalParks(app)
		})
		scheduler.Start()

		return nil
	})

	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}
