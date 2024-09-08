# parkpilot

Parkpilot is a Go-based web application designed to help users quickly choose a US National Park to visit based on their current location and real-time weather conditions at the parks. This project was inspired by the challenge of finding suitable national parks for camping trips during variable weather conditions, such as spring break.

### Features

- Utilizes data from the National Park Service API, OpenWeatherMap API, and Mapbox API
- Efficiently processes and stores large image files from the NPS API
- Optimizes API queries using geospatial calculations
- Provides a user-friendly interface for selecting national parks

### Tech Stack
- [Air](https://github.com/cosmtrek/air) - for live reloading
- [Go](https://go.dev/) - for server side
- [Pocketbase](https://pocketbase.io/) - for data management 
- [HTMX](https://htmx.org/) - for state management
- [templ](https://templ.guide/) - for templating
- [Tailwind CSS](https://tailwindcss.com/) - for styling

### Data Sources and Integration
- [National Park Service](https://www.nps.gov/subjects/developer/api-documentation.htm) - park details and information
- [OpenWeathermap](https://openweathermap.org/api) - real-time weather data
- [Mapbox](https://www.mapbox.com/) - interactive maps for park exploration

### Key Implementation Details

1. Image Processing: Large images (10MB+) from the NPS API are resized and compressed before storage to improve performance.
2. Data Optimization:
    - Certain API responses are stored as JSON strings in the database.
    - The Haversine function is used to narrow down parks within a specific radius of the user's location provided by the browser's Geolocation API minimizing the number of queries to Mapbox API.
    - Selected responses are cached in localStorage for improved performance.
3. Frontend:
    - HTMX is used for state management and dynamic content updates.
    - Additional client-side logic is implemented in pure JavaScript.

### Components

1. Backend (Go)
    - Handles API requests and data processing
    - Implements business logic for park selection
    - Manages database interactions
    - Processes and optimizes images
2. Frontend
    - Uses templ for server-side rendering
    - Implements HTMX for dynamic content updates
    - Utilizes pure JavaScript for additional client-side logic
3. Database (Pocketbase)
    - Stores park information, including processed images
    - Caches certain API responses as JSON strings
4. External APIs
    - National Park Service API: Provides park data and images
    - OpenWeatherMap API: Supplies real-time weather information
    - Mapbox API: Used for geolocation services
      
<img width="783" alt="Screenshot 2024-09-08 at 15 25 47" src="https://github.com/user-attachments/assets/c7c23a3e-7fca-4e73-8048-773e49d8c120">

--------

<img width="1245" alt="Screenshot 2024-09-08 at 15 48 11" src="https://github.com/user-attachments/assets/75200311-900e-4ba3-9bb6-2f115d06eb38">

### Data Flow

1. User accesses the application
2. Browser's Geolocation API provides user coordinates
3. Backend queries Pocketbase for parks within a certain radius (using Haversine function)
4. Weather data is fetched for relevant parks from OpenWeatherMap API
5. Park and weather data are combined and sent to the frontend
6. Frontend renders the park options using templ and HTMX
7. User selects a park
8. Additional park details are fetched and displayed
   
![chrome-capture-2024-9-8](https://github.com/user-attachments/assets/ad7b752f-a414-497d-850e-f8cc6266767a)

--------

![chrome-capture-2024-9-8 (1)](https://github.com/user-attachments/assets/94739bf5-ffa9-4675-8188-456f977b0c5f)

### Future Expansion
While the current version focuses on US National Parks, the project has the potential to include national parks worldwide. The main challenge for global expansion is the task of accumulating and consolidating data for the approximately 6,555 national parks worldwide (according to the web).

To starts run
```
air
```

Go to http://127.0.0.1:8090/
