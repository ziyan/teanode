---
name: weather
description: Weather forecast via the National Weather Service API (US locations)
tools:
  - name: get_weather
    description: Get weather forecast for a US location (free-form input like "Alpharetta, GA" or "Chicago, IL"). Returns geocoded coordinates, current conditions, and today's forecast from NWS.
    type: workflow
    timeout: 30
    parameters:
      type: object
      properties:
        location:
          type: string
          description: "Free-form location (e.g. 'Alpharetta, GA', 'Chicago, IL', 'Portland, OR')"
      required: ["location"]
    steps:
      - name: geocode
        type: http
        method: GET
        url: "https://nominatim.openstreetmap.org/search?q={{location}}&format=json&limit=1&countrycodes=us"
        headers:
          User-Agent: "TeaNode-Weather-Skill/1.0 (teanode-skills; github.com/teanode/teanode-skills)"
          Accept: application/json
        result: json
        select:
          lat: "0.lat"
          lon: "0.lon"
          display_name: "0.display_name"

      - name: points
        type: http
        method: GET
        url: "https://api.weather.gov/points/{{steps.geocode.lat}},{{steps.geocode.lon}}"
        headers:
          User-Agent: "TeaNode-Weather-Skill/1.0 (teanode-skills; github.com/teanode/teanode-skills)"
          Accept: application/geo+json
        result: json
        select:
          forecast: "properties.forecast"
          forecastHourly: "properties.forecastHourly"

      - name: hourly
        type: http
        method: GET
        url: "{{steps.points.forecastHourly}}"
        maxBytes: 300000
        headers:
          User-Agent: "TeaNode-Weather-Skill/1.0 (teanode-skills; github.com/teanode/teanode-skills)"
          Accept: application/geo+json
        result: json
        select:
          temperature: "properties.periods[0].temperature"
          temperatureUnit: "properties.periods[0].temperatureUnit"
          windSpeed: "properties.periods[0].windSpeed"
          windDirection: "properties.periods[0].windDirection"
          shortForecast: "properties.periods[0].shortForecast"
          humidity: "properties.periods[0].relativeHumidity.value"
          precipitationChance: "properties.periods[0].probabilityOfPrecipitation.value"

      - name: daily
        type: http
        method: GET
        url: "{{steps.points.forecast}}"
        maxBytes: 300000
        headers:
          User-Agent: "TeaNode-Weather-Skill/1.0 (teanode-skills; github.com/teanode/teanode-skills)"
          Accept: application/geo+json
        result: json
        select:
          highTemp: "properties.periods[0].temperature"
          isDaytime: "properties.periods[0].isDaytime"
          detail: "properties.periods[0].detailedForecast"
          todayPrecip: "properties.periods[0].probabilityOfPrecipitation.value"
          lowTemp: "properties.periods[1].temperature"
---

US weather forecast via Nominatim geocoding + NWS (api.weather.gov).
Report the weather using steps.geocode for location, steps.hourly for current conditions, and steps.daily for today's forecast. Data source: National Weather Service.
