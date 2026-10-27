package journal

import (
	"encoding/json"
	"math"
	"sort"
	"time"

	"github.com/ziyan/teanode/internal/health"
	"github.com/ziyan/teanode/internal/models"
)

// Health for a day. The numbers come from the summaries the phone worked out
// for the day, the workouts and State of Mind entries from the readings
// kept, and the steps by hour from the step readings when they are kept.

const (
	// healthSamplesMost is how many readings of one kind a day looks at.
	healthSamplesMost = 3000

	// workoutsMost and stateOfMindMost are how many of those kinds a day keeps.
	workoutsMost    = 50
	stateOfMindMost = 100
)

// sleepStageWords are the stages of a night as a day tells them, by the key
// of the day's seconds by stage.
var sleepStageWords = map[string]string{
	"asleepDeep": "deep", "asleepCore": "core", "asleepREM": "rem", "asleepUnspecified": "asleep",
	"awake": "awake", "inBed": "inBed",
}

// HealthMoodLevel maps a State of Mind to the journal's five moods. HealthKit
// gives each entry a valence from -1 (very unpleasant) to 1 (very pleasant);
// the day's mood is the average valence of its entries, laid on five steps:
// -1 is 1 (awful), -0.5 is 2 (low), 0 is 3 (okay), 0.5 is 4 (good), 1 is 5
// (great), each rounded to the nearest. No entries is 0, no mood.
func HealthMoodLevel(valences []float64) int {
	if len(valences) == 0 {
		return 0
	}
	total := 0.0
	for _, valence := range valences {
		total += math.Max(-1, math.Min(1, valence))
	}
	average := total / float64(len(valences))
	return int(math.Round((average+1)*2)) + 1
}

func (self *dayReader) daySummary(healthType string) (*models.HealthPeriodSummary, error) {
	periods, _, err := self.transaction.ListHealthPeriods(self.user.ID, healthType, self.day.Day, self.day.Day, health.PeriodDay)
	if err != nil || len(periods) == 0 {
		return nil, err
	}
	return periods[0], nil
}

func (self *dayReader) readHealth() error {
	result := &Health{}
	isFound := false

	steps, err := self.daySummary("stepCount")
	if err != nil {
		return err
	}
	if steps != nil && steps.SumValue != nil {
		result.StepCount, isFound = int(math.Round(*steps.SumValue)), true
	}
	walked, err := self.daySummary("distanceWalkingRunning")
	if err != nil {
		return err
	}
	if walked != nil && walked.SumValue != nil {
		result.WalkedMeters, isFound = *walked.SumValue, true
	}
	resting, err := self.daySummary("restingHeartRate")
	if err != nil {
		return err
	}
	if resting != nil && resting.AverageValue != nil {
		result.RestingHeartRate, isFound = *resting.AverageValue, true
	}
	sleep, err := self.daySummary(health.SleepAnalysisType)
	if err != nil {
		return err
	}
	if sleep != nil {
		minutes, stages := sleepOf(sleep)
		if minutes > 0 || len(stages) > 0 {
			result.SleepMinutes, result.SleepStages, isFound = minutes, stages, true
		}
	}

	from, until := self.day.From, self.day.Until
	workouts, err := self.transaction.ListHealthSamples(self.user.ID, "workout", &from, &until, workoutsMost, 0, false)
	if err != nil {
		return err
	}
	for _, sample := range workouts.Samples {
		workout := Workout{
			SampleID: sample.ID, ActivityType: sample.WorkoutActivityType, StartedAt: sample.StartedAt,
			Minutes: int(math.Round(sample.EndedAt.Sub(sample.StartedAt).Minutes())),
		}
		workout.Meters, _ = metadataNumber(sample.SampleMetadataJSON, "totalDistanceMeters")
		result.Workouts = append(result.Workouts, workout)
		isFound = true
	}
	sort.SliceStable(result.Workouts, func(left, right int) bool {
		return result.Workouts[left].StartedAt.Before(result.Workouts[right].StartedAt)
	})

	moods, err := self.transaction.ListHealthSamples(self.user.ID, "stateOfMind", &from, &until, stateOfMindMost, 0, false)
	if err != nil {
		return err
	}
	var valences []float64
	for _, sample := range moods.Samples {
		if valence, isSet := metadataNumber(sample.SampleMetadataJSON, "stateOfMindValence"); isSet {
			valences = append(valences, valence)
		}
	}
	result.MoodLevel = HealthMoodLevel(valences)
	if result.MoodLevel > 0 {
		isFound = true
	}

	stepReadings, err := self.transaction.ListHealthSamples(self.user.ID, "stepCount", &from, &until, healthSamplesMost, 0, false)
	if err != nil {
		return err
	}
	if len(stepReadings.Samples) > 0 {
		result.StepsByHour = stepsByHour(stepReadings.Samples, self.location)
	}

	if isFound {
		self.day.Health = result
	}
	return nil
}

// sleepOf is a night's minutes asleep and its stages in order. The minutes
// are the night's total time asleep when the day has it, which counts
// overlapping sources once, else the stages that are asleep added up, else
// the time the day says it lasted.
func sleepOf(summary *models.HealthPeriodSummary) (int, []SleepStage) {
	var stages []SleepStage
	asleepFromStages := 0.0
	for _, key := range health.StageOrder {
		seconds := summary.CategorySeconds[key]
		if seconds <= 0 {
			continue
		}
		stages = append(stages, SleepStage{SleepStage: sleepStageWords[key], Minutes: int(math.Round(seconds / 60))})
		if key != "awake" && key != "inBed" {
			asleepFromStages += seconds
		}
	}
	switch {
	case summary.CategorySeconds[health.SleepAsleepKey] > 0:
		return int(math.Round(summary.CategorySeconds[health.SleepAsleepKey] / 60)), stages
	case asleepFromStages > 0:
		return int(math.Round(asleepFromStages / 60)), stages
	case summary.DurationSeconds != nil:
		return int(math.Round(*summary.DurationSeconds / 60)), stages
	}
	return 0, stages
}

// stepsByHour adds step readings into the 24 hours of the day in its zone,
// each reading to the hour it started in.
func stepsByHour(samples []*models.HealthSample, location *time.Location) []int {
	hours := make([]int, 24)
	for _, sample := range samples {
		if sample.QuantityValue == nil {
			continue
		}
		// The hour of the clock is what a person reads, and a day with a clock
		// change is 23 or 25 hours long, so ask the clock and not the offset.
		hour := sample.StartedAt.In(location).Hour()
		if hour >= 0 && hour < 24 {
			hours[hour] += int(math.Round(*sample.QuantityValue))
		}
	}
	return hours
}

// metadataNumber reads one number out of a reading's metadata.
func metadataNumber(metadataJSON, key string) (float64, bool) {
	if metadataJSON == "" {
		return 0, false
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
		return 0, false
	}
	value, isNumber := metadata[key].(float64)
	return value, isNumber
}
