package journal_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/journal"
)

// weatherService is a server that answers like Open-Meteo and remembers what
// it was asked.
type weatherService struct {
	server  *httptest.Server
	paths   []string
	queries []map[string]string
	status  int
	body    string
}

func newWeatherService(t *testing.T, body string) *weatherService {
	t.Helper()
	service := &weatherService{status: http.StatusOK, body: body}
	service.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		query := map[string]string{}
		for name, values := range request.URL.Query() {
			query[name] = values[0]
		}
		service.paths, service.queries = append(service.paths, request.URL.Path), append(service.queries, query)
		writer.WriteHeader(service.status)
		_, _ = writer.Write([]byte(service.body))
	}))
	t.Cleanup(service.server.Close)
	return service
}

func (self *weatherService) urls() journal.WeatherURLs {
	return journal.WeatherURLs{ArchiveURL: self.server.URL + "/v1/archive", ForecastURL: self.server.URL + "/v1/forecast"}
}

const rainyDay = `{"daily":{"time":["2026-10-22"],"weather_code":[61],"temperature_2m_max":[14.2],"temperature_2m_min":[8.1],"precipitation_sum":[6.4]}}`

func TestFetchWeatherAsksTheArchiveForAPastDayAtARoundedPlace(t *testing.T) {
	service := newWeatherService(t, rainyDay)
	weather, err := journal.FetchWeather(context.Background(), http.DefaultClient, service.urls(), 40.7484, -73.9857, "2026-10-22", "America/New_York", false)
	if err != nil {
		t.Fatal(err)
	}
	if weather.WeatherCode != 61 || weather.HighCelsius != 14.2 || weather.LowCelsius != 8.1 || weather.PrecipitationMM != 6.4 || weather.FetchedAt.IsZero() {
		t.Errorf("the weather: %+v", weather)
	}
	if len(service.paths) != 1 || service.paths[0] != "/v1/archive" {
		t.Fatalf("a past day is asked of the archive: %v", service.paths)
	}
	query := service.queries[0]
	want := map[string]string{
		"latitude": "40.7", "longitude": "-74.0", "start_date": "2026-10-22", "end_date": "2026-10-22",
		"daily": "weather_code,temperature_2m_max,temperature_2m_min,precipitation_sum", "timezone": "America/New_York",
	}
	for name, value := range want {
		if query[name] != value {
			t.Errorf("%s was sent as %q, want %q", name, query[name], value)
		}
	}
	if len(query) != len(want) {
		t.Errorf("nothing else about the person is sent: %v", query)
	}
}

func TestFetchWeatherAsksTheForecastForToday(t *testing.T) {
	service := newWeatherService(t, rainyDay)
	if _, err := journal.FetchWeather(context.Background(), http.DefaultClient, service.urls(), 51.5, -0.12, "2026-10-22", "Europe/London", true); err != nil {
		t.Fatal(err)
	}
	if len(service.paths) != 1 || service.paths[0] != "/v1/forecast" || service.queries[0]["longitude"] != "-0.1" {
		t.Errorf("today is asked of the forecast: %v %v", service.paths, service.queries)
	}
}

func TestFetchWeatherRefusesWhatIsNotWeatherForTheDay(t *testing.T) {
	for name, scenario := range map[string]struct {
		body   string
		status int
		want   string
	}{
		"not yet":       {`{"daily":{"time":["2026-10-22"],"weather_code":[null],"temperature_2m_max":[null],"temperature_2m_min":[null],"precipitation_sum":[null]}}`, 200, "no weather for 2026-10-22 yet"},
		"another day":   {`{"daily":{"time":["2026-10-21"],"weather_code":[1],"temperature_2m_max":[1],"temperature_2m_min":[0],"precipitation_sum":[0]}}`, 200, "no weather"},
		"no daily":      {`{"reason":"out of range"}`, 200, "no weather"},
		"not json":      {`<html>`, 200, "not weather"},
		"an error":      {`{"error":true}`, 500, "answered 500"},
		"dry day":       {`{"daily":{"time":["2026-10-22"],"weather_code":[0],"temperature_2m_max":[20],"temperature_2m_min":[12],"precipitation_sum":[null]}}`, 200, ""},
		"rate limited":  {`{}`, 429, "answered 429"},
		"empty answers": {`{"daily":{"time":[],"weather_code":[],"temperature_2m_max":[],"temperature_2m_min":[]}}`, 200, "no weather"},
	} {
		t.Run(name, func(t *testing.T) {
			service := newWeatherService(t, scenario.body)
			service.status = scenario.status
			weather, err := journal.FetchWeather(context.Background(), http.DefaultClient, service.urls(), 40.7, -74, "2026-10-22", "UTC", false)
			if scenario.want == "" {
				if err != nil || weather.PrecipitationMM != 0 || weather.WeatherCode != 0 || weather.HighCelsius != 20 {
					t.Fatalf("a dry day is weather: %+v %v", weather, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), scenario.want) {
				t.Fatalf("want an error with %q, got %+v %v", scenario.want, weather, err)
			}
		})
	}
	for _, place := range [][2]float64{{91, 0}, {0, 181}, {-91, 0}} {
		if _, err := journal.FetchWeather(context.Background(), http.DefaultClient, journal.DefaultWeatherURLs, place[0], place[1], "2026-10-22", "UTC", false); err == nil {
			t.Errorf("%v is not a place", place)
		}
	}
	if _, err := journal.FetchWeather(context.Background(), http.DefaultClient, journal.DefaultWeatherURLs, 1, 1, "yesterday", "UTC", false); err == nil {
		t.Error("a day is YYYY-MM-DD")
	}
}

func TestTheDefaultWeatherServiceIsOpenMeteo(t *testing.T) {
	if journal.DefaultWeatherURLs.ArchiveURL != "https://archive-api.open-meteo.com/v1/archive" || journal.DefaultWeatherURLs.ForecastURL != "https://api.open-meteo.com/v1/forecast" {
		t.Errorf("the addresses: %+v", journal.DefaultWeatherURLs)
	}
}
