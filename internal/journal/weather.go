package journal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/safefetch"
)

// A day's weather, from Open-Meteo: a free weather service with an archive
// that goes back decades and covers the world, and no key to hold. What it is
// given is a place rounded to a tenth of a degree (about eleven kilometers)
// and a day; it is told nothing else about the person.

// WeatherURLs are the two addresses weather is asked from. A test points them
// at a server of its own.
type WeatherURLs struct {
	// ArchiveURL answers days that are over, ForecastURL today.
	ArchiveURL  string
	ForecastURL string
}

// DefaultWeatherURLs are Open-Meteo's.
var DefaultWeatherURLs = WeatherURLs{
	ArchiveURL:  "https://archive-api.open-meteo.com/v1/archive",
	ForecastURL: "https://api.open-meteo.com/v1/forecast",
}

// weatherAnswerMost is how much of an answer is read: a day of weather is a
// few hundred bytes.
const weatherAnswerMost = 1 << 20

// weatherDaily are the values asked for, which are also the keys they come
// back under.
const weatherDaily = "weather_code,temperature_2m_max,temperature_2m_min,precipitation_sum"

// WeatherFetcher fetches one day's weather at a place: the day, YYYY-MM-DD,
// in the zone given, and whether it is today, which is asked of the forecast
// and not the archive.
type WeatherFetcher func(ctx context.Context, latitude, longitude float64, day, timeZone string, isToday bool) (*models.JournalWeather, error)

// NewWeatherFetcher is the fetcher for a client and addresses. A nil client is
// the server's safe one, which refuses addresses inside the network; the
// addresses are the service's own, so that is only a guard against being
// pointed elsewhere.
func NewWeatherFetcher(client *http.Client, urls WeatherURLs) WeatherFetcher {
	return func(ctx context.Context, latitude, longitude float64, day, timeZone string, isToday bool) (*models.JournalWeather, error) {
		return FetchWeather(ctx, client, urls, latitude, longitude, day, timeZone, isToday)
	}
}

// roundToTenth is a coordinate rounded to a tenth of a degree, written with
// one place.
func roundToTenth(degrees float64) string {
	return strconv.FormatFloat(degrees, 'f', 1, 64)
}

// FetchWeather asks for the weather of one day at a place: the archive for a
// day that is over, the forecast for today. The place is rounded to a tenth of
// a degree before it is sent. A day the service has no values for yet (the
// archive lags a few days behind) is an error, so that it is asked again.
func FetchWeather(ctx context.Context, client *http.Client, baseURLs WeatherURLs, latitude, longitude float64, day, timeZone string, isToday bool) (*models.JournalWeather, error) {
	if _, err := ParseDay(day); err != nil {
		return nil, err
	}
	if latitude < -90 || latitude > 90 || longitude < -180 || longitude > 180 {
		return nil, fmt.Errorf("journal: %g, %g is not a place on earth", latitude, longitude)
	}
	if client == nil {
		client = safefetch.Client()
	}
	base := baseURLs.ArchiveURL
	if isToday {
		base = baseURLs.ForecastURL
	}
	target, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("journal: the weather address is not an address: %w", err)
	}
	query := target.Query()
	query.Set("latitude", roundToTenth(latitude))
	query.Set("longitude", roundToTenth(longitude))
	query.Set("start_date", day)
	query.Set("end_date", day)
	query.Set("daily", weatherDaily)
	if timeZone != "" {
		query.Set("timezone", timeZone)
	}
	target.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("journal: asking for the weather: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, weatherAnswerMost))
	if err != nil {
		return nil, fmt.Errorf("journal: reading the weather: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("journal: the weather service answered %d", response.StatusCode)
	}

	var answer struct {
		Daily struct {
			Time             []string   `json:"time"`
			WeatherCode      []*float64 `json:"weather_code"`
			TemperatureMax   []*float64 `json:"temperature_2m_max"`
			TemperatureMin   []*float64 `json:"temperature_2m_min"`
			PrecipitationSum []*float64 `json:"precipitation_sum"`
		} `json:"daily"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		return nil, fmt.Errorf("journal: the weather service answered something that is not weather: %w", err)
	}
	daily := answer.Daily
	if len(daily.Time) != 1 || daily.Time[0] != day || len(daily.WeatherCode) != 1 || len(daily.TemperatureMax) != 1 || len(daily.TemperatureMin) != 1 {
		return nil, fmt.Errorf("journal: the weather service has no weather for %s yet", day)
	}
	if daily.WeatherCode[0] == nil || daily.TemperatureMax[0] == nil || daily.TemperatureMin[0] == nil {
		return nil, fmt.Errorf("journal: the weather service has no weather for %s yet", day)
	}
	weather := &models.JournalWeather{
		WeatherCode: int(*daily.WeatherCode[0]), HighCelsius: *daily.TemperatureMax[0], LowCelsius: *daily.TemperatureMin[0],
		FetchedAt: time.Now(),
	}
	// Dry days come back as 0 and a day with no figure as null; no figure is
	// no rain to report.
	if len(daily.PrecipitationSum) == 1 && daily.PrecipitationSum[0] != nil {
		weather.PrecipitationMM = *daily.PrecipitationSum[0]
	}
	return weather, nil
}
